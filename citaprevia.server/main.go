package main

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"net/http"
	"os"
	"reflect"
	"strings"
	"text/template"
	"time"

	"github.com/SherClockHolmes/webpush-go"
	"github.com/alexedwards/argon2id"
	"github.com/avct/uasurfer"
	"github.com/canastic/ulidx"
	"github.com/gorilla/securecookie"
	"github.com/lib/pq"
	_ "github.com/lib/pq"
	"github.com/tcard/sqler"
)

var (
	serverAddr          = os.Getenv("CITAPREVIA_SERVER_ADDR")
	postgresConnString  = os.Getenv("CITAPREVIA_POSTGRES_CONNSTRING")
	authTokenHashKey    = os.Getenv("CITAPREVIA_AUTH_TOKEN_HASH_KEY")
	authTokenBlockKey   = os.Getenv("CITAPREVIA_AUTH_TOKEN_BLOCK_KEY")
	smsToKey            = os.Getenv("CITAPREVIA_SMSTO_KEY")
	pushVAPIDPublicKey  = os.Getenv("CITAPREVIA_PUSH_VAPID_PUBLIC_KEY")
	pushVAPIDPrivateKey = os.Getenv("CITAPREVIA_PUSH_VAPID_PRIVATE_KEY")
)

func main() {
	ctx := context.Background()

	startMonitorAPI(ctx)

	db, err := sql.Open("postgres", postgresConnString)
	if err != nil {
		panic(err)
	}
	dbx := sqler.WrapDB(db)

	go func() {
		(&delayAlertLoop{db: dbx}).run()
	}()

	s := http.Server{
		Addr:    serverAddr,
		Handler: server{dbx},
	}
	log(ctx).Printf("Serving at %s", serverAddr)
	log(ctx).Printf("err=%s", s.ListenAndServe())
}

type server struct {
	db sqler.DB
}

func (s server) ServeHTTP(w http.ResponseWriter, req *http.Request) {
	w.Header().Set("Access-Control-Allow-Origin", req.Header.Get("Origin"))
	if req.Method == http.MethodOptions {
		// w.Header().Set("Access-Control-Allow-Methods", "POST, GET, PUT, OPTIONS")
		w.Header().Set("Access-Control-Allow-Credentials", "true")
		// w.Header().Set("Access-Control-Allow-Headers", )
		return
	}

	ctx := req.Context()
	ctx = scope(ctx, "requestID", ulidx.New())
	req = req.WithContext(ctx)

	lw := &loggedResponseWriter{w: w}
	w = lw
	defer func(since time.Time) {
		status, _ := lw.HeaderSent()
		log(req.Context()).Printf("path=%s elapsed=%v status=%v", req.URL.Path, time.Since(since), status)
	}(time.Now().UTC())

	ua := uasurfer.Parse(req.Header.Get("User-Agent"))
	log(req.Context()).Printf("path=%s browser=%s os=%s device=%s",
		req.URL.Path,
		ua.Browser.Name.StringTrimPrefix(),
		ua.OS.Name.StringTrimPrefix(),
		ua.DeviceType.StringTrimPrefix(),
	)

	onErr500(s.serveHTTP).ServeHTTP(w, req)
}

var webFileServer = http.FileServer(http.Dir("/home/canastic/citaprevia.web"))
var landingFileServer = http.FileServer(http.Dir("/home/canastic/citaprevia.landing"))

func (s server) serveHTTP(w http.ResponseWriter, req *http.Request) error {
	if strings.HasPrefix(req.Host, "web.tengocita.app") {
		webFileServer.ServeHTTP(w, req)
		return nil
	}

	switch req.URL.Path {
	default:
		landingFileServer.ServeHTTP(w, req)
		return nil
	case "/signup":
		return s.serveAction(w, req, &signupAction{})
	case "/login":
		return s.serveAction(w, req, &loginAction{})
	case "/listActiveAppointments":
		return s.serveAction(w, req, &withBusinessAuth{action: &listActiveAppointmentsAction{}})
	case "/newAppointment":
		return s.serveAction(w, req, &withBusinessAuth{action: &newAppointmentAction{}})
	case "/startAppointment":
		return s.serveAction(w, req, &withBusinessAuth{action: &startAppointmentAction{}})
	case "/finishAppointment":
		return s.serveAction(w, req, &withBusinessAuth{action: &finishAppointmentAction{}})
	case "/cancelAppointment":
		return s.serveAction(w, req, &withBusinessAuth{action: &cancelAppointmentAction{}})
	case "/configureBusiness":
		return s.serveAction(w, req, &withBusinessAuth{action: &configureBusinessAction{}})
	case "/delayAlert":
		return s.serveAction(w, req, &withBusinessAuth{action: &delayAlertAction{}})
	case "/customerAppointment":
		return s.serveCustomerAppointment(w, req)
	case "/customer-service-worker.js":
		http.ServeContent(w, req, "customer-service-worker.js", time.Time{}, strings.NewReader(customerServiceWorkerJS))
		return nil
	case "/registerWebPush":
		return s.registerWebPush(w, req)
	case "/unsubscribePromoEmails":
		return s.unsubscribePromoEmails(w, req)
	case "/subscribePromoEmails":
		return s.subscribePromoEmails(w, req)
	}
}

type httpAction interface {
	serveAction(context.Context, server) (interface{}, error)
}

type withBusinessAuth struct {
	authToken string
	action    httpBusinessAction
}

type invalidAuthToken struct{}

func (a *withBusinessAuth) UnmarshalJSON(js []byte) error {
	err := json.Unmarshal(js, &struct {
		AuthToken *string `json:"authToken"`
	}{&a.authToken})
	if err != nil {
		return err
	}
	return json.Unmarshal(js, a.action)
}

func (a withBusinessAuth) serveAction(ctx context.Context, s server) (interface{}, error) {
	var sessionAuth sessionAuthentication
	err := secCookies.Decode("authToken", a.authToken, &sessionAuth)
	if err != nil {
		return invalidAuthToken{}, nil
	}
	var businessID string
	err = s.db.QueryRow(ctx, `
		UPDATE business_sessions
		SET
			last_used = now() at time zone 'utc'
		WHERE
			id = $1
		RETURNING business_id
		;
	`, sessionAuth.SessionID).Scan(&businessID)
	if errors.Is(err, sql.ErrNoRows) {
		return invalidAuthToken{}, nil
	}
	if err != nil {
		return nil, fmt.Errorf("checking auth token: %w", err)
	}

	ctx = scope(ctx, "businessID", businessID)

	result, err := a.action.serveAction(ctx, s, businessID)
	if err != nil {
		return nil, fmt.Errorf("businessID=%s: %w", businessID, err)
	}

	return result, nil
}

type httpBusinessAction interface {
	serveAction(_ context.Context, _ server, businessID string) (interface{}, error)
}

func (s server) serveAction(w http.ResponseWriter, req *http.Request, action httpAction) error {
	req = req.WithContext(scope(req.Context(), "action", reflect.TypeOf(action).Elem().Name()))

	err := json.NewDecoder(req.Body).Decode(action)
	if err != nil {
		return fmt.Errorf("unmarshaling action=%s: %w", req.URL.Path[1:], err)
	}
	defer req.Body.Close()

	resp, err := action.serveAction(req.Context(), s)
	if err != nil {
		return fmt.Errorf("on action=%s: %w", req.URL.Path[1:], err)
	}

	err = json.NewEncoder(w).Encode(struct {
		Result  string      `json:"result"`
		Payload interface{} `json:"payload,omitempty"`
	}{
		Result:  reflect.TypeOf(resp).Name(),
		Payload: resp,
	})
	if err != nil {
		return fmt.Errorf("marshaling response to action=%s: %w", req.URL.Path[1:], err)
	}

	log(req.Context()).Printf("result=%v", reflect.TypeOf(resp).Name())

	return nil
}

type signupAction struct {
	EmailOrPhone string `json:"emailOrPhone"`
	Password     string `json:"password"`
	PromoCode    string `json:"promoCode"`
}

type (
	missingEmailOrPhone struct{}
	missingPassword     struct{}
	emailOrPhoneTaken   struct{}
	badPromoCode        struct{}
	signedUp            struct {
		Business
		AuthToken string `json:"authToken"`
	}
)

func (a signupAction) serveAction(ctx context.Context, srv server) (interface{}, error) {
	a.EmailOrPhone = strings.TrimSpace(a.EmailOrPhone)
	a.Password = strings.TrimSpace(a.Password)
	a.PromoCode = strings.ToUpper(strings.TrimSpace(a.PromoCode))

	if a.EmailOrPhone == "" {
		return missingEmailOrPhone{}, nil
	}
	if a.Password == "" {
		return missingPassword{}, nil
	}
	if a.PromoCode != "" && a.PromoCode != "MIXXIO" {
		return badPromoCode{}, nil
	}

	var email, phone sql.NullString
	if strings.Contains(a.EmailOrPhone, "@") {
		email.String, email.Valid = a.EmailOrPhone, true
	} else {
		phone.String, phone.Valid = a.EmailOrPhone, true
	}

	id := ulidx.New()

	hashedPassword, err := argon2id.CreateHash(a.Password, argon2id.DefaultParams)
	if err != nil {
		return nil, err
	}

	var business Business
	if email.Valid {
		business.Email = &email.String
	}
	if phone.Valid {
		business.Phone = &phone.String
	}

	_, err = srv.db.Exec(ctx, `
		INSERT INTO businesses
			(id, email, phone, password, last_login, signup_promo_code)
		VALUES
			($1, $2, $3, $4, now() at time zone 'utc', $5)
	`, id, email, phone, hashedPassword, nilIfEmpty(a.PromoCode))

	var authToken string
	if isUniqueViolation(err) {
		// Graceful retry after newSession failure: attempt login.
		var err error
		var ok bool
		authToken, business, ok, err = srv.login(ctx, a.EmailOrPhone, a.Password)
		if err != nil {
			return nil, fmt.Errorf("logging in: %w", err)
		}
		if !ok {
			return emailOrPhoneTaken{}, nil
		}
	} else if err != nil {
		return nil, fmt.Errorf("inserting business: %w", err)
	} else {
		log(ctx).Printf("Signed up businessID=%s", id)

		var err error
		authToken, err = srv.newSession(ctx, id)
		if err != nil {
			return nil, fmt.Errorf("creating session: %w", err)
		}
	}

	return signedUp{
		AuthToken: authToken,
		Business:  business,
	}, nil
}

type loginAction struct {
	EmailOrPhone string `json:"emailOrPhone"`
	Password     string `json:"password"`
}

type (
	badCredentials struct{}
	loggedIn       struct {
		Business
		AuthToken string `json:"authToken"`
	}
)

type Business struct {
	Name    *string `json:"name,omitempty"`
	Email   *string `json:"email,omitempty"`
	Phone   *string `json:"phone,omitempty"`
	Address *string `json:"address,omitempty"`
}

func (a loginAction) serveAction(ctx context.Context, srv server) (interface{}, error) {
	a.EmailOrPhone = strings.TrimSpace(a.EmailOrPhone)
	a.Password = strings.TrimSpace(a.Password)

	if a.EmailOrPhone == "" {
		return missingEmailOrPhone{}, nil
	}
	if a.Password == "" {
		return missingPassword{}, nil
	}

	authToken, business, ok, err := srv.login(ctx, a.EmailOrPhone, a.Password)
	if err != nil {
		return nil, fmt.Errorf("logging in: %w", err)
	}
	if !ok {
		return badCredentials{}, nil
	}
	return loggedIn{
		AuthToken: authToken,
		Business:  business,
	}, nil
}

type listActiveAppointmentsAction struct {
	Start time.Time `json:"start"`
	End   time.Time `json:"end"`
}

type (
	missingStart struct{}
	missingEnd   struct{}
	appointments []Appointment
)

type Appointment struct {
	ID         string     `json:"id"`
	Number     int        `json:"number"`
	Start      time.Time  `json:"start"`
	End        time.Time  `json:"end"`
	Phone      *string    `json:"phone,omitempty"`
	Email      *string    `json:"email,omitempty"`
	Name       *string    `json:"name,omitempty"`
	Comments   *string    `json:"comments,omitempty"`
	StartedAt  *time.Time `json:"startedAt,omitempty"`
	FinishedAt *time.Time `json:"finishedAt,omitempty"`
	CanceledAt *time.Time `json:"canceledAt,omitempty"`
}

func (a listActiveAppointmentsAction) serveAction(ctx context.Context, srv server, businessID string) (interface{}, error) {
	if a.Start.IsZero() {
		return missingStart{}, nil
	}
	if a.End.IsZero() {
		return missingEnd{}, nil
	}

	rows, err := srv.db.Query(ctx, `
		SELECT
			id,
			number,
			start,
			"end",
			phone,
			email,
			name,
			started_at
		FROM appointments
		WHERE
			business_id = $1 AND "end" >= $2 AND start < $3
			AND canceled_at IS NULL AND finished_at IS NULL
		;
	`, businessID, a.Start, a.End)
	if err != nil {
		return nil, fmt.Errorf("fetching appointments for businessID=%v start=%v end=%v: %w", businessID, a.Start, a.End, err)
	}
	defer rows.Close()

	apps := appointments{}

	for rows.Next() {
		var app Appointment
		err := rows.Scan(
			&app.ID,
			&app.Number,
			&app.Start,
			&app.End,
			&app.Phone,
			&app.Email,
			&app.Name,
			&app.StartedAt,
		)
		if err != nil {
			return nil, fmt.Errorf("scanning row: %w", err)
		}
		apps = append(apps, app)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("scanning rows: %w", err)
	}

	return apps, nil
}

type newAppointmentAction struct {
	Start     time.Time `json:"start"`
	End       time.Time `json:"end"`
	Phone     string    `json:"phone,omitempty"`
	Email     string    `json:"email,omitempty"`
	Name      string    `json:"name,omitempty"`
	Commments string    `json:"comments,omitempty"`
}

type (
	// missingEmailOrPhone struct{}
	// missingStart struct{}
	created struct {
		CustomerMessage string `json:"customerMessage,omitempty"`
	}
)

func (a newAppointmentAction) serveAction(ctx context.Context, srv server, businessID string) (interface{}, error) {
	a.Phone = trimPhone(a.Phone)
	a.Email = strings.TrimSpace(a.Email)
	if a.Phone == "" && a.Email == "" {
		return missingEmailOrPhone{}, nil
	}
	if a.Start.IsZero() {
		return missingStart{}, nil
	}
	if a.End.IsZero() {
		a.End = a.Start.Add(10 * time.Minute) // TODO
	}
	a.Name = strings.TrimSpace(a.Name)

	tx, err := srv.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}

	var customerLink string

	var result interface{}
	err = useTx(tx, func(tx sqler.Tx) (commit bool, err error) {
		var number int64
		err = tx.QueryRow(ctx, `
			INSERT INTO last_appointment_number_for_day
				(business_id, day)
			VALUES
				($1, $2 :: date)
			ON CONFLICT (business_id, day) DO UPDATE SET
				number = last_appointment_number_for_day.number + 1
			RETURNING
				number
			;
		`, businessID, a.Start).Scan(&number)
		if err != nil {
			return false, fmt.Errorf("fetching appointment number: %w", err)
		}

		err = tx.QueryRow(ctx, `
			INSERT INTO appointments (
				business_id, id,
				start, "end",
				phone, email, number,
				name, comments
			) VALUES (
				$1, $2,
				$3, $4,
				$5, $6, $7,
				$8, $9
			)
			RETURNING
				customer_link
			;
		`,
			businessID, ulidx.New(),
			a.Start, a.End,
			nilIfEmpty(a.Phone), nilIfEmpty(a.Email), number,
			nilIfEmpty(a.Name), nilIfEmpty(a.Commments),
		).Scan(&customerLink)
		if err != nil {
			return false, fmt.Errorf("inserting appointment: %w", err)
		}

		var businessName string
		err = tx.QueryRow(context.Background(), `
			SELECT name FROM businesses WHERE id = $1;
		`, businessID).Scan(&businessName)
		if err != nil {
			return false, fmt.Errorf("fetching business name: %w", err)
		}
		_, m, d := a.Start.Date()
		customerMsg := fmt.Sprintf(
			`📆 Tienes cita con %s el %d/%d a las %d:%02d.

Para recibir avisos relacionados con tu cita, acepta recibir notificaciones aquí: https://tengocita.app/c/%s

Cuando llegues, muestra el código QR que encontrarás en el enlace.`,
			businessName, d, m, a.Start.Hour(), a.Start.Minute(), customerLink,
		)
		result = created{
			CustomerMessage: customerMsg,
		}
		return true, nil
	})

	if a.Phone != "" {
		// TODO: Do this in a proper queue.
		go func() {
			if true {
				return
			}

			_, m, d := a.Start.Date()
			f := fmt.Sprintf(
				`Cita el %d/%d a las %d:%02d con %%s. Muestra tu código aquí: https://tengocita.app/c/%s`,
				d, m, a.Start.Hour(), a.Start.Minute(), customerLink,
			)
			var name string
			for {
				err := srv.db.QueryRow(context.Background(), `
					SELECT name FROM businesses WHERE id = $1;
				`, businessID).Scan(&name)
				if err == nil {
					break
				}
				log(ctx).Printf("Error fetching business name for SMS businessID=%s err=%s", businessID, err)
				time.Sleep(5 * time.Second)
			}
			if len(name) > 160-len(f)-len(`%s`) {
				name = name[:len(f)-len(`%s`)-len(`...`)] + "..."
			}
			for {
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				err := sendSMS(ctx, a.Phone, fmt.Sprintf(f, name))
				cancel()
				if err == nil {
					break
				}
				log(ctx).Printf("Error sending SMS phone=%s err=%s", a.Phone, err)
				time.Sleep(5 * time.Second)
			}
		}()
	}
	// TODO: Send emails

	return result, err
}

type startAppointmentAction struct {
	Code int    `json:"code"`
	ID   string `json:"id"`
}

type (
	missingCodeOrID struct{}
	notFound        struct{}
	started         struct {
		Appointment Appointment   `json:"appointment"`
		MeanDelay   time.Duration `json:"meanDelay,omitempty"`
	}
)

func (a startAppointmentAction) serveAction(ctx context.Context, srv server, businessID string) (interface{}, error) {
	where := ""
	var params []interface{}
	if a.Code != 0 {
		where = "customer_code = $1 AND business_id = $2 AND (start AT TIME ZONE 'UTC') :: date = (now() at time zone 'utc') :: date"
		params = append(params, a.Code/100, businessID)
	} else if a.ID != "" {
		where = "business_id = $1 AND id = $2"
		params = append(params, businessID, a.ID)
	} else {
		return missingCodeOrID{}, nil
	}

	var app Appointment
	var delay time.Duration

	tx, err := srv.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}
	err = useTx(tx, func(tx sqler.Tx) (commit bool, err error) {
		err = tx.QueryRow(ctx, `
			UPDATE appointments SET started_at = COALESCE(appointments.started_at, $3)
			WHERE
				canceled_at IS NULL AND finished_at IS NULL AND `+where+`
			RETURNING
				id,
				number,
				start,
				"end",
				phone,
				email,
				name,
				started_at
			;
		`, append(params, now())...).Scan(
			&app.ID,
			&app.Number,
			&app.Start,
			&app.End,
			&app.Phone,
			&app.Email,
			&app.Name,
			&app.StartedAt,
		)
		if err != nil {
			return false, fmt.Errorf("fetching appointment for businessID=%v id=%v code=%v: %w", businessID, a.ID, a.Code, err)
		}

		alreadyAlerting := false
		err = tx.QueryRow(ctx, `
 			SELECT c > 0 FROM (SELECT COUNT(*) AS c FROM delay_alerts WHERE business_id = $1) q;
		`, businessID).Scan(&alreadyAlerting)
		if err != nil {
			return false, fmt.Errorf("checking if there's an alert already for businessID=%v: %w", businessID, err)
		}

		if !alreadyAlerting {
			var ok bool
			delay, ok, err = meanDelay(ctx, tx, businessID, 3)
			if err != nil {
				return false, fmt.Errorf("calculating mean delay for businessID=%v: %w", businessID, err)
			}
			if !ok {
				delay = 0
			}
		}

		return true, nil
	})
	if errors.Is(err, sql.ErrNoRows) {
		return notFound{}, nil
	}
	if err != nil {
		return nil, err
	}
	return started{
		Appointment: app,
		MeanDelay:   delay,
	}, nil
}

type finishAppointmentAction struct {
	ID string `json:"id"`
}

type (
// ok struct{}
)

func (a finishAppointmentAction) serveAction(ctx context.Context, srv server, businessID string) (interface{}, error) {
	_, err := srv.db.Exec(ctx, `
		UPDATE appointments SET finished_at = now() at time zone 'utc'
		WHERE
			business_id = $1 AND id = $2
			AND canceled_at IS NULL AND started_at IS NOT NULL AND finished_at IS NULL
		;
	`, businessID, a.ID)
	if err != nil {
		return nil, fmt.Errorf("finishing appointment for businessID=%v id=%v: %w", businessID, a.ID, err)
	}

	return ok{}, nil
}

type cancelAppointmentAction struct {
	ID     string `json:"id"`
	Reason string `json:"reason"`
}

type (
	canceled struct {
		CustomerMessage string `json:"customerMessage,omitempty"`
	}

// notFound
)

func (a cancelAppointmentAction) serveAction(ctx context.Context, srv server, businessID string) (interface{}, error) {
	tx, err := srv.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelSerializable})
	if err != nil {
		return nil, fmt.Errorf("beginning transaction: %w", err)
	}

	var result interface{}
	err = useTx(tx, func(tx sqler.Tx) (commit bool, err error) {
		var customerLink string
		var phone sql.NullString
		var pushSubJS []byte
		var day time.Time

		err = tx.QueryRow(ctx, `
			UPDATE appointments SET
				canceled_at = COALESCE(appointments.canceled_at, now() at time zone 'utc'),
				cancel_reason = $3
			WHERE
				business_id = $1 AND id = $2
				AND started_at IS NULL AND finished_at IS NULL
			RETURNING
				customer_link, phone, push_subscription, start
			;
		`, businessID, a.ID, nilIfEmpty(strings.TrimSpace(a.Reason))).Scan(
			&customerLink, &phone, &pushSubJS, &day,
		)
		if err != nil {
			return false, fmt.Errorf("cancel appointment for businessID=%v id=%v: %w", businessID, a.ID, err)
		}
		var pushSub *webpush.Subscription
		if len(pushSubJS) > 0 {
			json.Unmarshal(pushSubJS, &pushSub)
		}

		result = canceled{}
		if !phone.Valid && pushSub == nil {
			return true, nil
		}

		var businessName string
		err = srv.db.QueryRow(context.Background(), `
			SELECT name FROM businesses WHERE id = $1;
		`, businessID).Scan(&businessName)
		if err != nil {
			return false, fmt.Errorf("fetching business name for message: %w", err)
		}

		if phone.Valid {
			result = canceled{
				CustomerMessage: fmt.Sprintf(
					`🚫📆 Anulada tu cita con %s. Detalles: https://tengocita.app/c/%s`,
					businessName, customerLink,
				),
			}
		}

		if pushSub != nil {
			go func() {
				err := sendPush(pushSub, PushNotif{
					Title: "🚫📆 Cita anulada",
					Options: PushOptions{
						Body: fmt.Sprintf(
							"Tu cita con %s del %s ha sido anulada.",
							businessName,
							day.Format("2/1"),
						),
						Tag:                "cancelled:" + customerLink,
						RequireInteraction: true,
						Data: map[string]interface{}{
							"customerLink": customerLink,
						},
					},
				})
				if err != nil {
					log(ctx).Printf("Error sending push notification to %s: %s", pushSub.Endpoint, err)
				}
			}()
		}

		return true, nil
	})
	if err != nil {
		return nil, err
	}

	return result, nil
}

type configureBusinessAction struct {
	Name    string `json:"name"`
	Phone   string `json:"phone,omitempty"`
	Email   string `json:"email,omitempty"`
	Address string `json:"address,omitempty"`
}

type (
	missingName struct{}
	// missingEmailOrPhone
	emailTaken struct{}
	phoneTaken struct{}
	business   Business
)

func (a configureBusinessAction) serveAction(ctx context.Context, srv server, businessID string) (interface{}, error) {
	a.Name = strings.TrimSpace(a.Name)
	a.Email = strings.TrimSpace(a.Email)
	a.Phone = strings.TrimSpace(a.Phone)
	a.Address = strings.TrimSpace(a.Address)

	if a.Name == "" {
		return missingName{}, nil
	}
	if a.Email == "" && a.Phone == "" {
		return missingEmailOrPhone{}, nil
	}

	_, err := srv.db.Exec(ctx, `
		UPDATE businesses SET
			name = $2,
			email = $3,
			phone = $4,
			address = $5
		WHERE
			id = $1
		;
	`, businessID, a.Name, nilIfEmpty(a.Email), nilIfEmpty(a.Phone), nilIfEmpty(a.Address))
	if err != nil {
		if isUniqueViolation(err) {
			var pqErr *pq.Error
			errors.As(err, &pqErr)
			switch pqErr.Constraint {
			case "businesses_email_key":
				return emailTaken{}, nil
			case "businesses_phone_key":
				return phoneTaken{}, nil
			}
		}
		return nil, fmt.Errorf("updating business businessID=%v: %w", businessID, err)
	}

	return business{
		Name:    &a.Name,
		Phone:   nilIfEmpty(a.Phone),
		Email:   nilIfEmpty(a.Email),
		Address: nilIfEmpty(a.Address),
	}, nil
}

func (srv server) login(ctx context.Context, emailOrPhone, password string) (authToken string, business Business, ok bool, err error) {
	var businessID, hashedPassword string
	err = srv.db.QueryRow(ctx, `
		SELECT
			id, password,
			email, phone,
			name, address
		FROM businesses
		WHERE
			email = $1 OR phone = $1
		;
	`, emailOrPhone).Scan(
		&businessID, &hashedPassword,
		&business.Email, &business.Phone,
		&business.Name, &business.Address,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return "", Business{}, false, nil
		}
		return "", Business{}, false, fmt.Errorf("fetching business ID and password: %w", err)
	}

	ok, err = argon2id.ComparePasswordAndHash(password, hashedPassword)
	if err != nil {
		return "", Business{}, false, fmt.Errorf("matching password hash: %w", err)
	}
	if !ok {
		return "", Business{}, false, nil
	}

	authToken, err = srv.newSession(ctx, businessID)
	return authToken, business, err == nil, err
}

func (srv server) newSession(ctx context.Context, businessID string) (authToken string, err error) {
	sessionID := ulidx.New()

	tx, err := srv.db.BeginTx(ctx, nil)
	if err != nil {
		return "", fmt.Errorf("beginning transaction: %w", err)
	}
	err = useTx(tx, func(tx sqler.Tx) (commit bool, err error) {
		_, err = tx.Exec(ctx, `
			INSERT INTO business_sessions
				(business_id, id)
			VALUES
				($1, $2)
			;
		`, businessID, sessionID)
		if err != nil {
			return false, fmt.Errorf("inserting session: %w", err)
		}

		_, err = tx.Exec(ctx, `
			UPDATE businesses SET
				last_login = now() at time zone 'utc'
			WHERE businesses.id = $1
			;
		`, businessID)
		if err != nil {
			return false, fmt.Errorf("updating last login timestamp: %w", err)
		}

		return true, nil
	})
	if err != nil {
		return "", err
	}

	authToken, err = secCookies.Encode("authToken", sessionAuthentication{
		SessionID:  sessionID,
		BusinessID: businessID,
		Issued:     time.Now().UTC(),
	})
	if err != nil {
		return "", fmt.Errorf("encoding auth token: %w", err)
	}
	return authToken, nil
}

type sessionAuthentication struct {
	SessionID  string    `json:"sessionID"`
	BusinessID string    `json:"businessID"`
	Issued     time.Time `json:"issued"`
}

var secCookies = func() *securecookie.SecureCookie {
	must := func(b []byte, err error) []byte {
		if err != nil {
			panic(err)
		}
		return b
	}
	c := securecookie.New(
		must(base64.StdEncoding.DecodeString(authTokenHashKey)),
		must(base64.StdEncoding.DecodeString(authTokenBlockKey)),
	)
	c.SetSerializer(securecookie.JSONEncoder{})
	return c
}()

func (srv server) unsubscribePromoEmails(w http.ResponseWriter, req *http.Request) error {
	link := req.URL.Query().Get("id")
	res, err := srv.db.Exec(req.Context(), `
		UPDATE businesses SET
			can_send_promo_emails = false
		WHERE promo_emails_unsubscribe_link = $1
		;
	`, link)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintln(w, `Ha ocurrido un error inesperado. Inténtalo de nuevo o ponte en contacto con hola@tengocita.app.`)
		return fmt.Errorf("unsubscribing %q from promo emails: %w", link, err)
	}
	if affected, err := res.RowsAffected(); err != nil || affected == 0 {
		w.WriteHeader(404)
		fmt.Fprintln(w, `Enlace incorrecto. Ponte en contacto con hola@tengocita.app.`)
		return nil
	}

	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	unsubscribePromoEmailsTpl.ExecuteTemplate(w, "", struct {
		Link string
	}{
		Link: link,
	})

	return nil
}

var unsubscribePromoEmailsTpl = template.Must(template.New("").Parse(`
<p>Ya no recibirás más correos promocionales.</p>
<p><a href="/subscribePromoEmails?id={{.Link}}">Haz clic aquí para volver a suscribirte.</a></p>
`))

func (srv server) subscribePromoEmails(w http.ResponseWriter, req *http.Request) error {
	link := req.URL.Query().Get("id")
	res, err := srv.db.Exec(req.Context(), `
		UPDATE businesses SET
			can_send_promo_emails = true
		WHERE promo_emails_unsubscribe_link = $1
		;
	`, link)
	if err != nil {
		w.WriteHeader(500)
		fmt.Fprintln(w, `Ha ocurrido un error inesperado. Inténtalo de nuevo o ponte en contacto con hola@tengocita.app.`)
		return fmt.Errorf("subscribing %q to promo emails: %w", link, err)
	}
	if affected, err := res.RowsAffected(); err != nil || affected == 0 {
		w.WriteHeader(404)
		fmt.Fprintln(w, `Enlace incorrecto. Ponte en contacto con hola@tengocita.app.`)
		return nil
	}

	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	subscribePromoEmailsTpl.ExecuteTemplate(w, "", struct {
		Link string
	}{
		Link: link,
	})

	return nil
}

var subscribePromoEmailsTpl = template.Must(template.New("").Parse(`
<p>Ahora volverás a recibir correos promocionales.</p>
<p><a href="/unsubscribePromoEmails?id={{.Link}}">Haz clic aquí para desuscribirte.</a></p>
`))

func isUniqueViolation(err error) bool {
	const uniqueViolation = "23505"
	var pqErr *pq.Error
	return errors.As(err, &pqErr) && pqErr.Code == uniqueViolation
}

func nilIfEmpty(s string) *string {
	if s != "" {
		return &s
	}
	return nil
}

func trimPhone(s string) string {
	s = strings.ReplaceAll(s, "(", "")
	s = strings.ReplaceAll(s, ")", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ReplaceAll(s, ".", "")
	s = strings.ReplaceAll(s, " ", "")
	return s
}

func sendSMS(ctx context.Context, phone, msg string) error {
	if smsToKey == "" {
		log(ctx).Printf("Skipping SMS to %s: %s", phone, msg)
		return nil
	}

	if !strings.HasPrefix(phone, "+34") {
		phone = "+34" + phone
	}
	js, err := json.Marshal(map[string]interface{}{
		"message":   msg,
		"to":        phone,
		"sender_id": "TengoCita",
		// TODO: callback_url
	})
	if err != nil {
		panic(err)
	}
	req, err := http.NewRequestWithContext(ctx, "POST", "https://api.sms.to/sms/send", bytes.NewReader(js))
	if err != nil {
		panic(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+smsToKey)
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 && resp.StatusCode < 500 {
		body, _ := ioutil.ReadAll(resp.Body)
		log(ctx).Printf(fmt.Sprintf("POST https://api.sms.to/sms/send responded with %s; body: %+v", resp.Status, string(body)))
		return nil
	}
	if resp.StatusCode >= 500 {
		return fmt.Errorf("POST https://api.sms.to/sms/send responded with %s", resp.Status)
	}
	return nil
}

func startMonitorAPI(ctx context.Context) {
	f := os.NewFile(3, "monitor-api")
	if f == nil {
		return
	}

	listener, err := net.FileListener(f)
	if err != nil {
		log(ctx).Printf("Error listening: %s", err)
		return
	}

	log(ctx).Printf("Serving monitor API at %s", listener.Addr())

	mux := http.NewServeMux()
	mux.HandleFunc("/health", func(w http.ResponseWriter, req *http.Request) {
		json.NewEncoder(w).Encode("ok")
	})

	go func() {
		defer func() {
			err := listener.Close()
			log(ctx).Printf("Stopped serving monitor API at %s: %v", listener.Addr(), err)
		}()

		panic(http.Serve(listener, mux))
	}()
}

func useTx(tx sqler.Tx, f func(sqler.Tx) (commit bool, err error)) (err error) {
	var commit bool
	defer func() {
		if commit && err == nil {
			err = tx.Commit()
		} else {
			tx.Rollback()
		}
	}()
	commit, err = f(tx)
	return
}
