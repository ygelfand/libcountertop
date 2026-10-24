package dhcp

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

var granted = net.IP{192, 168, 1, 1}

func renewal(t *testing.T) *dhcpv4.DHCPv4 {
	t.Helper()

	msg, err := renewMessage(us, held, false, "speaker")
	if err != nil {
		t.Fatalf("renewMessage: %v", err)
	}
	return msg
}

func TestARenewalIsARequest(t *testing.T) {
	if got := renewal(t).MessageType(); got != dhcpv4.MessageTypeRequest {
		t.Errorf("a renewal is a %s", got)
	}
}

func TestTheAddressIsInCiaddrAndNotRequested(t *testing.T) {
	msg := renewal(t)

	if !msg.ClientIPAddr.Equal(held) {
		t.Errorf("ciaddr is %s, want %s", msg.ClientIPAddr, held)
	}
	if got := msg.RequestedIPAddress(); got != nil {
		t.Errorf("the requested-address option is set to %s", got)
	}
}

func TestARenewalNamesNoServer(t *testing.T) {
	for _, broadcasting := range []bool{false, true} {
		msg, err := renewMessage(us, held, broadcasting, "speaker")
		if err != nil {
			t.Fatal(err)
		}
		if got := msg.ServerIdentifier(); got != nil {
			t.Errorf("broadcasting=%v: names server %s", broadcasting, got)
		}
	}
}

func TestARenewalAsksForWhatTheLeaseIsBuiltFrom(t *testing.T) {
	asked := renewal(t).ParameterRequestList()
	for _, want := range []dhcpv4.OptionCode{
		dhcpv4.OptionSubnetMask,
		dhcpv4.OptionRouter,
		dhcpv4.OptionDomainNameServer,
		dhcpv4.OptionNTPServers,
	} {
		if !asked.Has(want) {
			t.Errorf("a renewal does not ask for %s", want)
		}
	}
}

func TestARenewalDoesNotAskToBeBroadcastAt(t *testing.T) {
	if renewal(t).IsBroadcast() {
		t.Error("a renewal asked for a broadcast reply")
	}
}

func TestRenewingSomethingThatIsNotIPv4IsRefused(t *testing.T) {
	if _, err := renewMessage(us, net.ParseIP("2001:db8::1"), false, "speaker"); err == nil {
		t.Error("renewing an IPv6 address was allowed")
	}
}

func ack(t *testing.T, opts ...dhcpv4.Modifier) *dhcpv4.DHCPv4 {
	t.Helper()

	msg, err := dhcpv4.New(append([]dhcpv4.Modifier{
		dhcpv4.WithMessageType(dhcpv4.MessageTypeAck),
		dhcpv4.WithYourIP(held),
		dhcpv4.WithNetmask(net.IPv4Mask(255, 255, 255, 0)),
		dhcpv4.WithOption(dhcpv4.OptServerIdentifier(granted)),
	}, opts...)...)
	if err != nil {
		t.Fatal(err)
	}
	return msg
}

func TestATimelessLeaseFallsBackToHalfAndSevenEighths(t *testing.T) {
	life := 2 * time.Hour
	got, err := leaseFrom(ack(t, dhcpv4.WithLeaseTime(uint32(life.Seconds()))))
	if err != nil {
		t.Fatal(err)
	}

	within := func(what string, at time.Time, want time.Duration) {
		t.Helper()
		if off := time.Until(at) - want; off < -time.Minute || off > time.Minute {
			t.Errorf("%s is %v away, want about %v", what, time.Until(at).Round(time.Second), want)
		}
	}
	within("renew", got.Renew, life/2)
	within("rebind", got.Rebind, life*7/8)
	within("expiry", got.Expires, life)
}

func TestARebindTimeThatIsNotAfterRenewIsPutInOrder(t *testing.T) {
	got, err := leaseFrom(ack(t,
		dhcpv4.WithLeaseTime(3600),
		dhcpv4.WithGeneric(dhcpv4.OptionRenewTimeValue, seconds(1800)),
		dhcpv4.WithGeneric(dhcpv4.OptionRebindingTimeValue, seconds(600)),
	))
	if err != nil {
		t.Fatal(err)
	}

	if !got.Rebind.After(got.Renew) {
		t.Errorf("rebind %v is not after renew %v", got.Rebind, got.Renew)
	}
	if !got.Expires.After(got.Rebind) {
		t.Errorf("expiry %v is not after rebind %v", got.Expires, got.Rebind)
	}
}

func TestALeaseWithNoMaskIsRefused(t *testing.T) {
	msg, err := dhcpv4.New(
		dhcpv4.WithMessageType(dhcpv4.MessageTypeAck),
		dhcpv4.WithYourIP(held),
	)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := leaseFrom(msg); err == nil {
		t.Error("a lease with no subnet mask was accepted")
	}
}

func seconds(v uint32) []byte {
	return []byte{byte(v >> 24), byte(v >> 16), byte(v >> 8), byte(v)}
}

func TestARebindReplyFromAnotherServerIsHeard(t *testing.T) {
	loop := net.IPv4(127, 0, 0, 1)
	listen := func() *net.UDPConn {
		c, err := net.ListenUDP("udp4", &net.UDPAddr{IP: loop})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = c.Close() })
		return c
	}
	asked, other, client := listen(), listen(), listen()

	msg := renewal(t)
	go func() {
		buf := make([]byte, 1500)
		_ = asked.SetDeadline(time.Now().Add(2 * time.Second))
		n, from, err := asked.ReadFromUDP(buf)
		if err != nil {
			return
		}
		req, err := dhcpv4.FromBytes(buf[:n])
		if err != nil {
			return
		}
		reply := ack(t, dhcpv4.WithTransactionID(req.TransactionID), dhcpv4.WithLeaseTime(3600))
		_, _ = other.WriteToUDP(reply.ToBytes(), from)
	}()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	got, err := exchange(ctx, client, asked.LocalAddr().(*net.UDPAddr), msg, held)
	if err != nil {
		t.Fatalf("exchange: %v", err)
	}
	if !got.Address.IP.Equal(held) {
		t.Errorf("leased %s, want %s", got.Address.IP, held)
	}
}

func TestRetransmitWaitsHalfTheRemainderButAtLeastAMinute(t *testing.T) {
	for _, tc := range []struct{ left, want time.Duration }{
		{4 * time.Hour, 2 * time.Hour},
		{90 * time.Second, 60 * time.Second},
		{30 * time.Second, 30 * time.Second},
	} {
		if got := retransmitAfter(tc.left); got != tc.want {
			t.Errorf("retransmitAfter(%v) = %v, want %v", tc.left, got, tc.want)
		}
	}
}
