package api

import "testing"

func TestValidEndpoint(t *testing.T) {
	ok := []string{"https://fcm.googleapis.com/fcm/send/x", "https://updates.push.services.mozilla.com/wpush/v2/x"}
	bad := []string{"", "http://fcm.googleapis.com/x", "https://localhost:9999/x", "https://127.0.0.1/x", "не адрес"}
	for _, e := range ok {
		if err := validEndpoint(e); err != nil {
			t.Errorf("%q отвергнут: %v", e, err)
		}
	}
	for _, e := range bad {
		if err := validEndpoint(e); err == nil {
			t.Errorf("%q принят", e)
		}
	}
}
