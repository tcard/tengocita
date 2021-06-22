package main

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/tcard/gock"
	"github.com/tcard/sqler"
)

func now() time.Time {
	// return time.Now().UTC()
	return time.Date(2020, 5, 13,
		14, 05,
		0, 0, time.UTC)
}

const (
	alertWindow         = 2 * time.Hour
	delayAlertThreshold = 5 * time.Minute
)

func meanDelay(ctx context.Context, db sqler.Queryer, businessID string, sample int) (time.Duration, bool, error) {
	var meanDelaySecs sql.NullFloat64
	err := db.QueryRow(ctx, `
		SELECT
			CASE WHEN count(*) = $1 THEN extract(epoch from avg(started_at - start)) ELSE NULL END
		FROM (
			SELECT *
			FROM appointments
			WHERE
				business_id = $2 AND started_at IS NOT NULL
				AND "end" >= ($3 :: date) AND start < ($3 :: date + interval '1' day)
			ORDER BY started_at DESC
			LIMIT $1
		) q
		;
	`, sample, businessID, now()).Scan(&meanDelaySecs)
	return time.Second * time.Duration(meanDelaySecs.Float64), meanDelaySecs.Valid, err
}

type delayAlertAction struct{}

type (
	ok struct{}
)

func (a delayAlertAction) serveAction(ctx context.Context, srv server, businessID string) (interface{}, error) {
	_, err := srv.db.Exec(ctx, fmt.Sprintf(`
		INSERT INTO delay_alerts (
			business_id
		) VALUES (
			$1
		)
		ON CONFLICT (business_id) DO NOTHING;
	`), businessID)
	if err != nil {
		return nil, fmt.Errorf("inserting delay alert: %w", err)
	}

	return ok{}, nil
}

type delayAlertLoop struct {
	db sqler.DB

	queuesMtx sync.Mutex
	queues    map[string][]delayState
}

func (l *delayAlertLoop) run() {
	ctx := context.Background()
	ctx = scope(ctx, "service", "delayAlerts")

	l.queues = make(map[string][]delayState)

	for {
		time.Sleep(5 * time.Minute)

		states, err := l.fetch(ctx)
		if err != nil {
			log(ctx).Printf("%s", err)
			continue
		}

		l.runAlerts(ctx, states)
	}
}

func (l *delayAlertLoop) fetch(ctx context.Context) ([]delayState, error) {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()

	rows, err := l.db.Query(ctx, `
		SELECT
			business_id, b.name, checking_started, extract(epoch from last_delay), last_start_cutoff
		FROM delay_alerts da
		JOIN businesses b ON da.business_id = b.id;
	`)
	if err != nil {
		return nil, fmt.Errorf("selecting alerts: %w", err)
	}
	defer rows.Close()

	var states []delayState
	for rows.Next() {
		var s delayState
		var lastDelaySecs sql.NullFloat64
		err := rows.Scan(&s.businessID, &s.businessName, &s.checkingStarted, &lastDelaySecs, &s.lastStartCutoff)
		if err != nil {
			return nil, fmt.Errorf("scanning alert: %w", err)
		}
		if lastDelaySecs.Valid {
			d := time.Second * time.Duration(lastDelaySecs.Float64)
			s.lastDelay = &d
		}
		states = append(states, s)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("fetching next alert: %w", err)
	}

	return states, nil
}

func (l *delayAlertLoop) runAlerts(ctx context.Context, states []delayState) {
	l.queuesMtx.Lock()
	defer l.queuesMtx.Unlock()

	for _, state := range states {
		ctx := scope(ctx, "businessID", state.businessID)

		state := state
		q, ok := l.queues[state.businessID]
		if ok {
			l.queues[state.businessID] = append(q, state)
			return
		}

		l.queues[state.businessID] = []delayState{}

		go func() {
			for {
				err := l.runAlert(ctx, state)
				if err != nil {
					var errs gock.ConcurrentErrors
					if !errors.As(err, &errs) {
						errs.Errors = []error{err}
					}
					for _, err := range errs.Errors {
						log(ctx).Printf("Error running alert: %s", err)
					}
					// Continue dequeing; next alert window will include this one.
				}

				l.queuesMtx.Lock()
				defer l.queuesMtx.Unlock()
				q := l.queues[state.businessID]
				if len(q) == 0 {
					delete(l.queues, state.businessID)
					return
				}
				state, l.queues[state.businessID] = q[0], q[1:]
				time.Sleep(1 * time.Minute)
			}
		}()
	}
}

func (l *delayAlertLoop) runAlert(ctx context.Context, state delayState) error {
	var delay time.Duration
	var hasDelay bool

	err := func() error {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()

		var err error
		delay, hasDelay, err = meanDelay(ctx, l.db, state.businessID, 1)
		if err != nil {
			return fmt.Errorf("fetching delay: %w", err)
		}
		hasDelay = delay > delayAlertThreshold
		return nil
	}()
	if err != nil {
		return err
	}

	log(ctx).Printf("checkingStarted=%v lastDelay=%v lastCutoff=%v delay=%v",
		state.checkingStarted,
		state.lastDelay,
		state.lastStartCutoff,
		delay,
	)

	var rows sqler.Rows

	err = func() error {
		var start, end time.Time
		if hasDelay && state.lastDelay != nil && time.Duration(math.Abs(float64(delay-*state.lastDelay))) < 5*time.Minute {
			// Same delay; only notify those we didn't notify before.
			start = *state.lastStartCutoff
			end = now().Add(alertWindow)
		} else {
			// Delay has changed; notify everybody who was notified before, plus
			// the new ones within the window.
			if state.checkingStarted != nil {
				start = *state.checkingStarted
			} else {
				start = now()
			}
			if !hasDelay && state.lastStartCutoff != nil {
				// Don't expand the window if we don't have a delay anymore;
				// notify only those who were notified of a delay before.
				end = *state.lastStartCutoff
			} else {
				end = now().Add(alertWindow)
			}
		}

		var err error
		rows, err = l.db.Query(ctx, `
			SELECT
				email, phone, push_subscription
			FROM appointments a
			WHERE
				business_id = $1
				AND started_at IS NULL AND finished_at IS NULL and canceled_at IS NULL
				AND start >= ($2 :: timestamptz)
				AND start <= ($3 :: timestamptz)
			;
		`, state.businessID, start, end)
		if err != nil {
			return fmt.Errorf("selecting appointments: %w", err)
		}
		return nil
	}()
	if err != nil {
		return err
	}
	defer rows.Close()

	var errs error

	for rows.Next() {
		var email, phone sql.NullString
		var pushSubJS []byte

		err := rows.Scan(&email, &phone, &pushSubJS)
		if err != nil {
			return fmt.Errorf("scanning appointment: %w", err)
		}
		var pushSub *webpush.Subscription
		if len(pushSubJS) > 0 {
			json.Unmarshal(pushSubJS, &pushSub)
		}

		errs = gock.AddConcurrentError(errs, func() error {
			if pushSub == nil {
				// TODO
				_ = phone
				_ = email
				return nil
			}

			var notif PushNotif
			if hasDelay {
				notif = PushNotif{
					Title: fmt.Sprintf(
						"%s Retraso en tu cita",
						clockEmojiForDelay(delay),
					),
					Options: PushOptions{
						Body: fmt.Sprintf(
							"Tu cita con %s va con %v de retraso aproximado.",
							state.businessName, delay.Truncate(time.Minute),
						),
						Actions: []PushAction{{
							Action: "go",
							Title:  "Ver cita",
						}, {
							Action: "cancel",
							Title:  "Anular cita",
						}},
					},
				}
			} else {
				notif = PushNotif{
					Title: fmt.Sprintf(
						"✅ Cita en su hora",
					),
					Options: PushOptions{
						Body: fmt.Sprintf(
							"Tu cita con %s ya no va con retraso.",
							state.businessName,
						),
						Actions: []PushAction{{
							Action: "go",
							Title:  "Ver cita",
						}},
					},
				}
			}

			err := sendPush(pushSub, notif)
			if err != nil {
				return fmt.Errorf("sending push notification to %s: %w", pushSub.Endpoint, err)
			}

			return nil
		}())
	}
	if err := rows.Err(); err != nil {
		return gock.AddConcurrentError(errs, fmt.Errorf("fetching next appointment: %w", err))
	}

	if !hasDelay {
		ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
		defer cancel()
		_, err := l.db.Exec(ctx, `
				DELETE FROM delay_alerts
				WHERE business_id = $1;
			`, state.businessID)
		if err != nil {
			return gock.AddConcurrentError(errs, fmt.Errorf("deleting alert: %w", err))
		}
		return errs
	}

	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	_, err = l.db.Exec(ctx, `
		UPDATE delay_alerts SET
			checking_started = COALESCE(checking_started, $3),
			last_delay = $2,
			last_start_cutoff = $4
		WHERE business_id = $1;
	`, state.businessID, delay/time.Second, now(), now().Add(alertWindow))
	if err != nil {
		return gock.AddConcurrentError(errs, fmt.Errorf("updating alert: %w", err))
	}

	return errs
}

var clockEmojis = []string{
	"🕐",
	"🕑",
	"🕒",
	"🕓",
	"🕔",
	"🕕",
	"🕖",
	"🕗",
	"🕘",
	"🕙",
	"🕚",
	"🕛",
}

func clockEmojiForDelay(delay time.Duration) string {
	return clockEmojis[delay%time.Hour/(10*time.Minute)]
}

type delayState struct {
	businessID      string
	businessName    string
	checkingStarted *time.Time
	lastDelay       *time.Duration
	lastStartCutoff *time.Time
}
