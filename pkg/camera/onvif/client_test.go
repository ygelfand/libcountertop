package onvif

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/ygelfand/libcountertop/pkg/network/digest"
)

func TestDeviceURLUsesTheTypedAddress(t *testing.T) {
	for _, tt := range []struct{ host, want string }{
		{"10.10.70.1", "http://10.10.70.1/onvif/device_service"},
		{"10.0.0.9:2020", "http://10.0.0.9:2020/onvif/device_service"},
		{"door.lan", "http://door.lan/onvif/device_service"},
		{"fd00::5", "http://[fd00::5]/onvif/device_service"},
		{"[fd00::5]:8000", "http://[fd00::5]:8000/onvif/device_service"},
	} {
		if got := DeviceURL(tt.host); got != tt.want {
			t.Errorf("DeviceURL(%q) = %q, want %q", tt.host, got, tt.want)
		}
	}
}

func TestTheClientFollowsCapabilitiesToTheStreamAndEvents(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		b := string(body)
		if !strings.Contains(b, "PasswordDigest") {
			http.Error(w, "NotAuthorized", http.StatusBadRequest)
			return
		}
		base := "http://" + r.Host
		switch {
		case strings.Contains(b, "GetCapabilities"):
			answer(w, `<tds:GetCapabilitiesResponse><tds:Capabilities>`+
				`<tt:Media><tt:XAddr>`+base+`/media</tt:XAddr></tt:Media>`+
				`<tt:Events><tt:XAddr>`+base+`/events</tt:XAddr></tt:Events>`+
				`</tds:Capabilities></tds:GetCapabilitiesResponse>`)
		case strings.Contains(b, "GetProfiles"):
			answer(w, `<trt:GetProfilesResponse>`+
				`<trt:Profiles token="000"><tt:VideoEncoderConfiguration><tt:Encoding>H265</tt:Encoding>`+
				`<tt:Resolution><tt:Width>2560</tt:Width><tt:Height>1920</tt:Height></tt:Resolution>`+
				`</tt:VideoEncoderConfiguration></trt:Profiles>`+
				`<trt:Profiles token="001"><tt:VideoEncoderConfiguration><tt:Encoding>H264</tt:Encoding>`+
				`<tt:Resolution><tt:Width>640</tt:Width><tt:Height>480</tt:Height></tt:Resolution>`+
				`</tt:VideoEncoderConfiguration></trt:Profiles></trt:GetProfilesResponse>`)
		case strings.Contains(b, "GetStreamUri"):
			token := b[strings.Index(b, "<ProfileToken>")+len("<ProfileToken>"):]
			token = token[:strings.Index(token, "<")]
			answer(
				w,
				`<trt:GetStreamUriResponse><trt:MediaUri><tt:Uri>rtsp://10.0.0.5:7447/stream`+token+
					`</tt:Uri></trt:MediaUri></trt:GetStreamUriResponse>`,
			)
		case strings.Contains(b, "CreatePullPointSubscription"):
			answer(w, `<tev:CreatePullPointSubscriptionResponse><tev:SubscriptionReference>`+
				`<wsa:Address>`+base+`/pull</wsa:Address></tev:SubscriptionReference></tev:CreatePullPointSubscriptionResponse>`)
		case strings.Contains(b, "PullMessages"):
			answer(w, `<tev:PullMessagesResponse><wsnt:NotificationMessage>`+
				`<wsnt:Topic>tns1:RuleEngine/MyRuleDetector/Visitor</wsnt:Topic>`+
				`<wsnt:Message><tt:Message><tt:Data><tt:SimpleItem Name="State" Value="true"/></tt:Data></tt:Message></wsnt:Message>`+
				`</wsnt:NotificationMessage></tev:PullMessagesResponse>`)
		default:
			answer(w, "")
		}
	}))
	defer srv.Close()
	ctx := context.Background()
	c, err := Dial(ctx, srv.URL+"/onvif/device_service", "admin", "pw")
	if err != nil {
		t.Fatal(err)
	}
	profiles, err := c.Profiles(ctx)
	if err != nil || len(profiles) != 2 || profiles[0].Encoding != "H265" ||
		profiles[1].Width != 640 {
		t.Fatalf("profiles %+v, %v", profiles, err)
	}
	uri, err := c.StreamURI(ctx, profiles[1].Token)
	if err != nil || uri != "rtsp://10.0.0.5:7447/stream001" {
		t.Fatalf("uri %q, %v", uri, err)
	}
	pull, err := c.Subscribe(ctx)
	if err != nil {
		t.Fatal(err)
	}
	notes, err := pull.Pull(ctx)
	if err != nil || len(notes) != 1 || !notes[0].On ||
		!strings.HasSuffix(notes[0].Topic, "Visitor") {
		t.Fatalf("notes %+v, %v", notes, err)
	}
	if _, err := Dial(ctx, srv.URL+"/onvif/device_service", "", ""); err != ErrRefused {
		t.Fatalf("no credentials gave %v, want ErrRefused", err)
	}
}

func TestStillAnswersADigestChallenge(t *testing.T) {
	gate := digest.New("door")
	creds := digest.Credentials(func() (string, string) { return "admin", "secret" })
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !gate.Allows(r.Header.Get("Authorization"), r.Method, creds) {
			w.Header().Set("WWW-Authenticate", gate.Header())
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		_, _ = io.WriteString(w, "jpeg")
	}))
	defer srv.Close()
	for _, tt := range []struct {
		pass string
		want string
		err  error
	}{
		{pass: "secret", want: "jpeg"},
		{pass: "wrong", err: ErrRefused},
	} {
		c := &Client{http: srv.Client(), user: "admin", pass: tt.pass}
		got, err := c.Still(context.Background(), srv.URL+"/snap.jpg?channel=1")
		if string(got) != tt.want || !errors.Is(err, tt.err) {
			t.Errorf("password %q: got %q, %v; want %q, %v", tt.pass, got, err, tt.want, tt.err)
		}
	}
}

func answer(w http.ResponseWriter, body string) {
	w.Header().Set("Content-Type", "application/soap+xml")
	_, _ = io.WriteString(
		w,
		`<?xml version="1.0"?><s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope"><s:Body>`+
			body+`</s:Body></s:Envelope>`,
	)
}
