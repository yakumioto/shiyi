package api_test

import (
	"encoding/json"
	"testing"

	"shiyi/internal/api"
)

func TestAuthenticationErrorStatus(t *testing.T) {
	for _, tc := range []struct {
		code string
		want int
	}{
		{api.CodeUnauthorized, 401},
		{api.CodeForbidden, 403},
		{api.CodeRateLimited, 429},
		{api.CodeAuthUnavailable, 503},
	} {
		t.Run(tc.code, func(t *testing.T) {
			if got := api.HTTPStatusForCode(tc.code); got != tc.want {
				t.Fatalf("HTTP status = %d, want %d", got, tc.want)
			}
		})
	}
}

func TestSessionResponseWireFormat(t *testing.T) {
	body, err := json.Marshal(api.SessionResponse{
		Authenticated: false,
		Mode:          api.AuthModePassword,
		CSRFToken:     "browser-bound-csrf-token",
	})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"authenticated":false,"mode":"password","csrf_token":"browser-bound-csrf-token"}`
	if string(body) != want {
		t.Fatalf("session response = %s, want %s", body, want)
	}
}
