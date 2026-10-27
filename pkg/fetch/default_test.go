package fetch

import (
	"io"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestVerificationPolicyIsScopedToFetchClients(t *testing.T) {
	strict(t)
	original := http.DefaultTransport
	server := httptest.NewTLSServer(
		http.HandlerFunc(
			func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok")) },
		),
	)
	defer server.Close()
	client := Client(0)
	Verify(true)
	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("untrusted certificate accepted")
	}
	Verify(false)
	response, err := client.Get(server.URL)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, response.Body); err != nil {
		t.Fatal(err)
	}
	response.Body.Close()
	for _, ordinary := range []*http.Client{{}, http.DefaultClient} {
		if response, err := ordinary.Get(server.URL); err == nil {
			response.Body.Close()
			t.Fatal("fetch policy changed an ordinary client's verification")
		}
	}
	if http.DefaultTransport != original {
		t.Fatal("fetch policy replaced http.DefaultTransport")
	}
	Verify(true)
	if response, err := client.Get(server.URL); err == nil {
		response.Body.Close()
		t.Fatal("strict mode reused insecure policy")
	}
}
