package main

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/skip2/go-qrcode"
)

func (srv server) serveCustomerAppointment(w http.ResponseWriter, req *http.Request) error {
	ctx := req.Context()

	var key string
	if req.Method == "POST" {
		req.ParseForm()
		key = req.Form.Get("customerLink")
	} else {
		key = req.URL.Query().Get("key")
	}
	if key == "" {
		http.Redirect(w, req, "https://tengocita.app", http.StatusSeeOther)
		return nil
	}

	if req.Method == "POST" {
		_, err := srv.db.Exec(ctx, `
			UPDATE appointments SET
				canceled_at = now() at time zone 'utc',
				cancel_reason = $2
			WHERE
				customer_link = $1
				AND started_at IS NULL
				AND canceled_at IS NULL
				AND finished_at IS NULL
			;
		`, key, nilIfEmpty(strings.TrimSpace(req.Form.Get("comments"))))
		if err != nil {
			return fmt.Errorf("cancel appointment customerLink=%v: %w", key, err)
		}
	}

	var app struct {
		Business struct {
			Email   *string
			Phone   *string
			Name    string
			Address *string
			Photo   *string
		}

		ID           string
		Start        time.Time
		CustomerCode int
		CustomerLink string
		StartedAt    *time.Time
		CanceledAt   *time.Time
		CancelReason *string
		FinishedAt   *time.Time
		Comments     *string

		LastDelay *time.Duration
	}

	var lastDelaySecs sql.NullFloat64
	err := srv.db.QueryRow(ctx, `
		SELECT
			b.email, b.phone, b.name, b.address, b.photo,
			a.id, a.start, a.customer_code, a.customer_link,
			a.started_at, a.canceled_at, a.cancel_reason, a.finished_at, a.comments,
			extract(epoch from da.last_delay)
		FROM
			businesses b
			JOIN appointments a ON b.id = a.business_id
			LEFT JOIN delay_alerts da
				ON b.id = da.business_id
				AND da.last_delay IS NOT NULL
		WHERE
			a.customer_link = $1
		;
	`, key).Scan(
		&app.Business.Email, &app.Business.Phone, &app.Business.Name, &app.Business.Address, &app.Business.Photo,
		&app.ID, &app.Start, &app.CustomerCode, &app.CustomerLink,
		&app.StartedAt, &app.CanceledAt, &app.CancelReason, &app.FinishedAt, &app.Comments,
		&lastDelaySecs,
	)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			http.Redirect(w, req, "https://tengocita.app", http.StatusSeeOther)
			return nil
		}
		return err
	}
	if lastDelaySecs.Valid {
		d := time.Second * time.Duration(lastDelaySecs.Float64)
		app.LastDelay = &d
	}

	w.Header().Set("Content-Type", "text/html;charset=utf-8")
	return customerLinkTpl.Execute(w, app)
}

func (srv server) registerWebPush(w http.ResponseWriter, req *http.Request) error {
	defer req.Body.Close()

	var body struct {
		AppointmentCustomerLink string          `json:"appointmentCustomerLink"`
		Subscription            json.RawMessage `json:"subscription"`
	}
	err := json.NewDecoder(req.Body).Decode(&body)
	if err != nil {
		return err
	}

	_, err = srv.db.Exec(req.Context(), `
		UPDATE appointments SET
			push_subscription = $1
		WHERE customer_link = $2
		;
	`, string(body.Subscription), body.AppointmentCustomerLink)
	if err != nil {
		return fmt.Errorf("setting push subscription for appointment %s: %w", body.AppointmentCustomerLink, err)
	}

	return nil
}

func qrPNGBase64(s string) string {
	q, _ := qrcode.Encode(s, qrcode.High, -30)
	return base64.StdEncoding.EncodeToString(q)
}

func customerCodeWithChecksum(code int) int {
	return code*100 + (code / 1000) + 2*(code/100%10) + 3*(code/10%10) + 4*(code%10)
}

var customerLinkTpl = template.Must(template.New("").Funcs(template.FuncMap{
	"qrPNGBase64":      qrPNGBase64,
	"codeWithChecksum": customerCodeWithChecksum,
	"nl2br": func(s string) template.HTML {
		return template.HTML(strings.ReplaceAll(html.EscapeString(s), "\n", "<br>"))
	},
}).Parse(`
<html>

<head>
<title>Tu cita con {{.Business.Name}}</title>
<meta name="viewport" content="width=device-width, initial-scale=1">
<style type="text/css">
body {
	font-family: sans-serif;
	text-align: center;
}

.alert {
	background-color: #ffffaa;
	padding: 20px;
}

button, input[type=submit] {
	padding: 10;
	font-weight: bold;
	font-size: 1em;
}
</style>
</head>

<body>

{{ if .CanceledAt }}

<h1>Cita cancelada</h1>

{{ with .CancelReason }}
<p><string>Motivo:</string></p>
<p>{{nl2br .}}</p>
{{ end }}

{{ else if .FinishedAt }}

<h1>Cita finalizada</h1>

{{ else }}

<script>
function urlBase64ToUint8Array(base64String) {
	var padding = '='.repeat((4 - base64String.length % 4) % 4);
	var base64 = (base64String + padding)
		.replace(/\-/g, '+')
		.replace(/_/g, '/');

	var rawData = window.atob(base64);
	var outputArray = new Uint8Array(rawData.length);

	for (var i = 0; i < rawData.length; ++i) {
		outputArray[i] = rawData.charCodeAt(i);
	}
	return outputArray;
}

var pushPK = '` + pushVAPIDPublicKey + `';

navigator.serviceWorker.register('/customer-service-worker.js');

function registerPush() {
	if (document.getElementById('activate-push')) {
		document.body.removeChild(document.getElementById('activate-push'));
	}

	Notification.requestPermission()
	.then(function(permission) {
		if (permission !== 'granted') {
			var div = document.createElement('div');
			div.id = 'activate-push';
			div.class = 'alert';
			div.innerHTML = '<h3>¿Quieres recibir avisos sobre tu cita? (Retrasos, anulación...)</h3>';

			var button = document.createElement('button');
			button.setAttribute('onclick', 'registerPush()');
			button.innerText = 'Sí, recibir';
			div.appendChild(button);

			document.body.insertBefore(div, document.body.firstChild);

			return null;
		}
		return navigator.serviceWorker.ready;
	})
	.then(function() {
		return navigator.serviceWorker.getRegistration()
	})
	.then(function(registration) {
		return registration.pushManager.getSubscription()
		.then(function(subscription) {
			if (subscription) {
				return subscription;
			}
			return registration.pushManager.subscribe({
				userVisibleOnly: true,
				applicationServerKey: urlBase64ToUint8Array(pushPK),
			});
		});
	})
	.then(function(subscription) {
		return fetch('/registerWebPush', {
			method: 'post',
			headers: {
				'Content-type': 'application/json'
			},
			body: JSON.stringify({
				appointmentCustomerLink: '{{.CustomerLink}}',
				subscription: subscription,
			}),
		});
	})
	.catch(function(err) {
		if (err instanceof DOMException) {
			return;
		}
		console.error(err);
		alert('Error al suscribirse a avisos sobre la cita. Actualiza la página para reintentarlo.');
	});
}

registerPush();

function toggleCancelForm() {
	var form = document.getElementById('cancel-form');
	form.innerHTML = '<h5>¿Seguro que quieres anular la cita?</h5>' +
		'<form method="post" action="">' +
		'<input type="hidden" name="customerLink" value="{{.CustomerLink}}">' +
		'<p><textarea name="comments" cols="40" rows="10" placeholder="Comentario sobre la anulación (motivo, solicitar nueva hora, etc.)"></textarea></p>' +
		'<p><input type="submit" style="background-color: red; color: white;" value="Sí, anular"></p>' +
		'</form>';
};
</script>

{{ with .LastDelay }}
<p class="alert">⚠️ Tu cita va con un retraso aproximado de <strong>{{.}}</strong>.</p>
{{ end }}

<h1>Tu cita</h1>

<p>Enseña este código al llegar:</p>

<img style="width: 90%;" src="data:image/png;base64, {{qrPNGBase64 .ID}}">

<p>O di tu código numérico:</p>

<h1 style="letter-spacing: 10px;">{{codeWithChecksum .CustomerCode}}</h1>

{{ end }}

<h3>Detalle de la cita</h3>

<table style="margin: 0 auto;">

<tr>
<td colspan="2">{{.Business.Name}}</td>
</tr>

<tr>
<td>🕑</td>
<td>{{.Start.Format "2 / 1 / 2006 a las 3:04" }}</td>
</tr>

{{with .Business.Phone}}
<tr>
<td>📞</td>
<td><a href="tel:{{.}}">{{.}}</a></td>
</tr>
{{end}}

{{with .Business.Email}}
<tr>
<td><strong>@</strong></td>
<td><a href="mailto:{{.}}">{{.}}</a></td>
</tr>
{{end}}

{{with .Business.Address}}
<tr>
<td>🌍</td>
<td><a href="http://maps.google.com/maps?q={{.}}">{{.}}</a></td>
</tr>
{{end}}

</table>

{{with .Comments}}
<h4>Comentarios</h4>

<p>{{nl2br .}}</p>
{{end}}

{{ if and (not .FinishedAt) (not .CanceledAt) }}
<div id="cancel-form">
<button style="background-color: red; color: white;" onclick="toggleCancelForm()">Anular la cita</button>
</div>
{{ end }}

</body>

</html>
`))
