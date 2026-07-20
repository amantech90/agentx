package update

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestCompareAndNormalize(t *testing.T) {
	t.Parallel()
	cases := []struct {
		a, b string
		want int
	}{
		{"v1.0.0", "1.0.0", 0},
		{"1.2.0", "1.1.9", 1},
		{"1.0.0", "1.0.1", -1},
		{"2.0.0", "1.9.9", 1},
		{"1.0.0-beta", "1.0.0", 0}, // pre-release normalizes to base
		{"1.0", "1.0.0", 0},        // missing component treated as 0
		{"1.0.0", "1.0", 0},
	}
	for _, tc := range cases {
		if got := compare(normalize(tc.a), normalize(tc.b)); got != tc.want {
			t.Errorf("compare(%q,%q) = %d, want %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func newChecker(url string) *Checker {
	return &Checker{endpoint: url, client: http.DefaultClient}
}

func TestCheckReportsNewerRelease(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v1.3.0","html_url":"https://example.test/r","body":"New stuff"}`))
	}))
	defer server.Close()

	info, err := newChecker(server.URL).Check(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !info.Available || info.Latest != "1.3.0" || info.Current != "1.0.0" || info.URL != "https://example.test/r" {
		t.Fatalf("unexpected info: %+v", info)
	}
}

func TestCheckReportsNoUpdateWhenCurrent(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v1.0.0","html_url":"https://example.test/r"}`))
	}))
	defer server.Close()

	info, err := newChecker(server.URL).Check(context.Background(), "v1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Available {
		t.Fatalf("expected no update, got %+v", info)
	}
}

func TestCheckIgnoresPrerelease(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v2.0.0","prerelease":true,"html_url":"https://example.test/r"}`))
	}))
	defer server.Close()

	info, err := newChecker(server.URL).Check(context.Background(), "1.0.0")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if info.Available {
		t.Fatalf("prerelease should not trigger an update, got %+v", info)
	}
}

func TestCheckDoesNotErrorOnServerFailure(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "rate limited", http.StatusForbidden)
	}))
	defer server.Close()

	info, err := newChecker(server.URL).Check(context.Background(), "1.0.0")
	if err == nil {
		t.Fatal("expected an error from a 403 response")
	}
	if info.Available {
		t.Fatalf("failed check must not report an update, got %+v", info)
	}
}
