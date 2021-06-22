package main

import (
	"encoding/json"
	"fmt"
	"io/ioutil"

	"github.com/SherClockHolmes/webpush-go"
)

type PushNotif struct {
	Title   string      `json:"title"`
	Options PushOptions `json:"options"`
}

type PushOptions struct {
	Body               string       `json:"body,omitempty"`
	Tag                string       `json:"tag,omitempty"`
	Actions            []PushAction `json:"actions,omitempty"`
	RequireInteraction bool         `json:"requireInteraction,omitempty"`
	Data               interface{}  `json:"data,omitempty"`
}

type PushAction struct {
	Action string `json:"action"`
	Title  string `json:"title,omitempty"`
	Icon   string `json:"icon,omitempty"`
}

func sendPush(sub *webpush.Subscription, data interface{}) error {
	js, err := json.Marshal(data)
	if err != nil {
		panic(err)
	}
	resp, err := webpush.SendNotification(js, sub, &webpush.Options{
		RecordSize:      2000,
		VAPIDPublicKey:  pushVAPIDPublicKey,
		VAPIDPrivateKey: pushVAPIDPrivateKey,
		TTL:             86400,
	})
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := ioutil.ReadAll(resp.Body)
		return fmt.Errorf("non-OK response  with status %d; body: %s", resp.StatusCode, body)
	}
	return nil
}

const customerServiceWorkerJS = `
self.addEventListener('push', function(event) {
	var notif = event.data.json();
	event.waitUntil(self.registration.showNotification(notif.title, notif.options));
});

self.addEventListener('notificationclick', function(event) {
	clients.openWindow('https://tengocita.app/c/' + event.notification.data.customerLink);
});
`
