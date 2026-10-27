package onvif

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha1"
	"encoding/base64"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/ygelfand/libcountertop/pkg/fetch"
)

const (
	discoveryPort = "3702"
	findWait      = 3 * time.Second
	callWait      = 20 * time.Second
	maxReply      = 1 << 20
	pullFor       = 10 * time.Second
	pullLimit     = 20
	subscribeLife = 10 * time.Minute
)

var subscribeFor = fmt.Sprintf("PT%dS", int(subscribeLife.Seconds()))

var (
	ErrRefused    = errors.New("onvif: wrong user or password")
	ErrNotFound   = errors.New("onvif: no device answered discovery")
	ErrNoService  = errors.New("onvif: the device does not offer that service")
	ErrNoProfiles = errors.New("onvif: the device has no media profiles")
)

type Client struct {
	http   *http.Client
	user   string
	pass   string
	device string
	media  string
	events string
}

type MediaProfile struct {
	Token    string
	Encoding string
	Width    int
	Height   int
	Source   string
	Config   string
}

type Notice struct {
	Topic   string
	On      bool
	Sources []string
}

type PullPoint struct {
	c  *Client
	at string
}

func Find(ctx context.Context, host string) (string, error) {
	return findAt(ctx, net.JoinHostPort(host, discoveryPort))
}

func findAt(ctx context.Context, addr string) (string, error) {
	to, err := net.ResolveUDPAddr("udp4", addr)
	if err != nil {
		return "", err
	}
	conn, err := net.ListenUDP("udp4", nil)
	if err != nil {
		return "", err
	}
	defer conn.Close()
	deadline := time.Now().Add(findWait)
	if d, ok := ctx.Deadline(); ok && d.Before(deadline) {
		deadline = d
	}
	_ = conn.SetDeadline(deadline)
	probe := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>`+
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://schemas.xmlsoap.org/ws/2004/08/addressing" xmlns:d="http://schemas.xmlsoap.org/ws/2005/04/discovery" xmlns:dn="http://www.onvif.org/ver10/network/wsdl">`+
		`<s:Header><a:MessageID>urn:uuid:%s</a:MessageID><a:To s:mustUnderstand="1">urn:schemas-xmlsoap-org:ws:2005:04:discovery</a:To>`+
		`<a:Action s:mustUnderstand="1">http://schemas.xmlsoap.org/ws/2005/04/discovery/Probe</a:Action></s:Header>`+
		`<s:Body><d:Probe><d:Types>dn:NetworkVideoTransmitter</d:Types></d:Probe></s:Body></s:Envelope>`, NewUUID())
	if _, err := conn.WriteToUDP([]byte(probe), to); err != nil {
		return "", err
	}
	buf := make([]byte, maxDatagram)
	for {
		n, from, err := conn.ReadFromUDP(buf)
		if err != nil {
			return "", errors.Join(ErrNotFound, err)
		}
		if !from.IP.Equal(to.IP) {
			continue
		}
		var m struct {
			XAddrs string `xml:"Body>ProbeMatches>ProbeMatch>XAddrs"`
		}
		if xml.Unmarshal(buf[:n], &m) != nil {
			continue
		}
		if x := pickAddr(strings.Fields(m.XAddrs), to.IP.String()); x != "" {
			return x, nil
		}
	}
}

func pickAddr(addrs []string, host string) string {
	for _, a := range addrs {
		if u, err := url.Parse(a); err == nil && u.Hostname() == host {
			return a
		}
	}
	if len(addrs) > 0 {
		return addrs[0]
	}
	return ""
}

func Dial(ctx context.Context, device, user, pass string) (*Client, error) {
	c := &Client{http: fetch.Client(callWait), user: user, pass: pass, device: device}
	var r struct {
		Media  string `xml:"Body>GetCapabilitiesResponse>Capabilities>Media>XAddr"`
		Events string `xml:"Body>GetCapabilitiesResponse>Capabilities>Events>XAddr"`
	}
	body := `<GetCapabilities xmlns="http://www.onvif.org/ver10/device/wsdl"><Category>All</Category></GetCapabilities>`
	if err := c.call(ctx, device, "", body, &r); err != nil {
		return nil, err
	}
	c.media, c.events = strings.TrimSpace(r.Media), strings.TrimSpace(r.Events)
	return c, nil
}

func (c *Client) Profiles(ctx context.Context) ([]MediaProfile, error) {
	if c.media == "" {
		return nil, ErrNoService
	}
	var r struct {
		Profiles []struct {
			Token string `xml:"token,attr"`
			Video struct {
				Encoding string `xml:"Encoding"`
				Width    int    `xml:"Resolution>Width"`
				Height   int    `xml:"Resolution>Height"`
			} `xml:"VideoEncoderConfiguration"`
			Source struct {
				Token  string `xml:"token,attr"`
				Source string `xml:"SourceToken"`
			} `xml:"VideoSourceConfiguration"`
		} `xml:"Body>GetProfilesResponse>Profiles"`
	}
	body := `<GetProfiles xmlns="http://www.onvif.org/ver10/media/wsdl"/>`
	if err := c.call(ctx, c.media, "", body, &r); err != nil {
		return nil, err
	}
	var out []MediaProfile
	for _, p := range r.Profiles {
		out = append(out, MediaProfile{
			Token:    p.Token,
			Encoding: strings.TrimSpace(p.Video.Encoding),
			Width:    p.Video.Width,
			Height:   p.Video.Height,
			Source:   strings.TrimSpace(p.Source.Source),
			Config:   p.Source.Token,
		})
	}
	if len(out) == 0 {
		return nil, ErrNoProfiles
	}
	return out, nil
}

func (c *Client) VideoSources(ctx context.Context) ([]string, error) {
	if c.media == "" {
		return nil, ErrNoService
	}
	var r struct {
		Sources []struct {
			Token string `xml:"token,attr"`
		} `xml:"Body>GetVideoSourcesResponse>VideoSources"`
	}
	body := `<GetVideoSources xmlns="http://www.onvif.org/ver10/media/wsdl"/>`
	if err := c.call(ctx, c.media, "", body, &r); err != nil {
		return nil, err
	}
	out := make([]string, len(r.Sources))
	for i, s := range r.Sources {
		out[i] = s.Token
	}
	return out, nil
}

func (c *Client) StreamURI(ctx context.Context, token string) (string, error) {
	if c.media == "" {
		return "", ErrNoService
	}
	var r struct {
		URI string `xml:"Body>GetStreamUriResponse>MediaUri>Uri"`
	}
	body := `<GetStreamUri xmlns="http://www.onvif.org/ver10/media/wsdl">` +
		`<StreamSetup><Stream xmlns="http://www.onvif.org/ver10/schema">RTP-Unicast</Stream>` +
		`<Transport xmlns="http://www.onvif.org/ver10/schema"><Protocol>RTSP</Protocol></Transport></StreamSetup>` +
		`<ProfileToken>` + escape(token) + `</ProfileToken></GetStreamUri>`
	if err := c.call(ctx, c.media, "", body, &r); err != nil {
		return "", err
	}
	if r.URI == "" {
		return "", ErrNoService
	}
	return strings.TrimSpace(r.URI), nil
}

func (c *Client) Subscribe(ctx context.Context) (*PullPoint, error) {
	if c.events == "" {
		return nil, ErrNoService
	}
	var r struct {
		Address string `xml:"Body>CreatePullPointSubscriptionResponse>SubscriptionReference>Address"`
	}
	body := `<CreatePullPointSubscription xmlns="http://www.onvif.org/ver10/events/wsdl">` +
		`<InitialTerminationTime>` + subscribeFor + `</InitialTerminationTime></CreatePullPointSubscription>`
	if err := c.call(ctx, c.events, "", body, &r); err != nil {
		return nil, err
	}
	if r.Address == "" {
		return nil, ErrNoService
	}
	return &PullPoint{c: c, at: strings.TrimSpace(r.Address)}, nil
}

func (p *PullPoint) Pull(ctx context.Context) ([]Notice, error) {
	var r struct {
		Messages []struct {
			Topic string `xml:"Topic"`
			Items []struct {
				Name  string `xml:"Name,attr"`
				Value string `xml:"Value,attr"`
			} `xml:"Message>Message>Data>SimpleItem"`
			Source []struct {
				Value string `xml:"Value,attr"`
			} `xml:"Message>Message>Source>SimpleItem"`
		} `xml:"Body>PullMessagesResponse>NotificationMessage"`
	}
	body := fmt.Sprintf(`<PullMessages xmlns="http://www.onvif.org/ver10/events/wsdl">`+
		`<Timeout>PT%dS</Timeout><MessageLimit>%d</MessageLimit></PullMessages>`,
		int(pullFor.Seconds()), pullLimit)
	if err := p.c.call(ctx, p.at, p.at, body, &r); err != nil {
		return nil, err
	}
	var out []Notice
	for _, m := range r.Messages {
		var sources []string
		for _, s := range m.Source {
			sources = append(sources, s.Value)
		}
		for _, it := range m.Items {
			if strings.EqualFold(it.Name, "State") || strings.EqualFold(it.Name, "IsMotion") {
				out = append(out, Notice{
					Topic:   strings.TrimSpace(m.Topic),
					On:      strings.EqualFold(it.Value, "true"),
					Sources: sources,
				})
			}
		}
	}
	return out, nil
}

func (p *PullPoint) Renew(ctx context.Context) error {
	return p.c.call(
		ctx,
		p.at,
		p.at,
		`<Renew xmlns="http://docs.oasis-open.org/wsn/b-2"><TerminationTime>`+subscribeFor+`</TerminationTime></Renew>`,
		nil,
	)
}

func RenewEvery() time.Duration { return subscribeLife / 2 }

func (p *PullPoint) Close(ctx context.Context) error {
	return p.c.call(
		ctx,
		p.at,
		p.at,
		`<Unsubscribe xmlns="http://docs.oasis-open.org/wsn/b-2"/>`,
		nil,
	)
}

func (c *Client) call(ctx context.Context, endpoint, to, body string, into any) error {
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		endpoint,
		strings.NewReader(c.envelope(to, body)),
	)
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/soap+xml; charset=utf-8")
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(io.LimitReader(resp.Body, maxReply))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusUnauthorized || bytes.Contains(data, []byte("NotAuthorized")) {
		return ErrRefused
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("onvif: %s answered %d", endpoint, resp.StatusCode)
	}
	if into == nil {
		return nil
	}
	return xml.Unmarshal(data, into)
}

func (c *Client) envelope(to, body string) string {
	var head strings.Builder
	head.WriteString(`<s:Header>`)
	if to != "" {
		head.WriteString(`<a:To s:mustUnderstand="1">` + escape(to) + `</a:To>`)
	}
	if c.user != "" {
		nonce := make([]byte, 16)
		_, _ = rand.Read(nonce)
		created := time.Now().UTC().Format(time.RFC3339)
		sum := sha1.Sum(append(append(nonce, created...), c.pass...))
		head.WriteString(
			`<Security s:mustUnderstand="1" xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-secext-1.0.xsd">` +
				`<UsernameToken><Username>` + escape(
				c.user,
			) + `</Username>` +
				`<Password Type="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-username-token-profile-1.0#PasswordDigest">` +
				base64.StdEncoding.EncodeToString(
					sum[:],
				) + `</Password>` +
				`<Nonce EncodingType="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-soap-message-security-1.0#Base64Binary">` +
				base64.StdEncoding.EncodeToString(
					nonce,
				) + `</Nonce>` +
				`<Created xmlns="http://docs.oasis-open.org/wss/2004/01/oasis-200401-wss-wssecurity-utility-1.0.xsd">` +
				created + `</Created></UsernameToken></Security>`,
		)
	}
	head.WriteString(`</s:Header>`)
	return `<?xml version="1.0" encoding="UTF-8"?>` +
		`<s:Envelope xmlns:s="http://www.w3.org/2003/05/soap-envelope" xmlns:a="http://www.w3.org/2005/08/addressing">` +
		head.String() + `<s:Body>` + body + `</s:Body></s:Envelope>`
}
