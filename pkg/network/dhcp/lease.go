package dhcp

import (
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

func keeping(addr net.IP, prefix uint8, want net.IP, wantPrefix uint8) bool {
	if want == nil || addr == nil {
		return false
	}
	return prefix == wantPrefix && addr.Equal(want)
}

type Lease struct {
	Address net.IPNet
	Router  net.IP
	DNS     []net.IP
	NTP     []net.IP
	Domain  string
	Server  net.IP

	// RFC 2131 4.4.5: T1 defaults to half the lease, T2 to seven eighths.
	Expires time.Time
	Renew   time.Time
	Rebind  time.Time
}

func leaseFrom(ack *dhcpv4.DHCPv4) (*Lease, error) {
	mask := ack.SubnetMask()
	if mask == nil {
		return nil, fmt.Errorf("dhcp: the lease carries no subnet mask")
	}

	lease := &Lease{
		Address: net.IPNet{IP: ack.YourIPAddr, Mask: mask},
		DNS:     ack.DNS(),
		NTP:     ack.NTPServers(),
		Domain:  ack.DomainName(),
		Server:  ack.ServerIdentifier(),
	}
	if routers := ack.Router(); len(routers) > 0 {
		lease.Router = routers[0]
	}

	now := time.Now()
	life := ack.IPAddressLeaseTime(time.Hour)
	lease.Expires = now.Add(life)

	lease.Renew = now.Add(life / 2)
	if t1 := ack.IPAddressRenewalTime(0); t1 > 0 {
		lease.Renew = now.Add(t1)
	}

	lease.Rebind = now.Add(life * 7 / 8)
	if t2 := ack.IPAddressRebindingTime(0); t2 > 0 {
		lease.Rebind = now.Add(t2)
	}

	// Some servers send T2 at or before T1.

	if !lease.Rebind.After(lease.Renew) {
		lease.Rebind = lease.Renew.Add(life / 8)
	}
	if !lease.Expires.After(lease.Rebind) {
		lease.Expires = lease.Rebind
	}
	return lease, nil
}

func (l Lease) String() string {
	return fmt.Sprintf("%s via %s, dns %s, for %s",
		l.Address.String(), l.Router, joinIPs(l.DNS), time.Until(l.Expires).Round(time.Second))
}

func (l *Lease) Nameservers() []string {
	out := make([]string, 0, len(l.DNS))
	for _, ip := range l.DNS {
		out = append(out, ip.String())
	}
	return out
}

func joinIPs(ips []net.IP) string {
	if len(ips) == 0 {
		return "(none)"
	}
	s := ips[0].String()
	for _, ip := range ips[1:] {
		s += ", " + ip.String()
	}
	return s
}
