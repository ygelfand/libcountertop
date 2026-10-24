package dhcp

import (
	"context"
	"errors"
	"fmt"
	"net"
	"time"

	"github.com/insomniacslk/dhcp/dhcpv4"
)

var errRefused = errors.New("dhcp: the server refused the renewal")

// RFC 2131 table 5: RENEWING and REBINDING set ciaddr, never requested-address or server identifier.
func renewMessage(
	mac net.HardwareAddr,
	addr net.IP,
	broadcasting bool,
	hostname string,
) (*dhcpv4.DHCPv4, error) {
	if addr.To4() == nil {
		return nil, fmt.Errorf("dhcp: %s is not an IPv4 address", addr)
	}

	msg, err := dhcpv4.New(
		dhcpv4.WithHwAddr(mac),
		dhcpv4.WithMessageType(dhcpv4.MessageTypeRequest),
		dhcpv4.WithOption(dhcpv4.OptHostName(hostname)),
		dhcpv4.WithRequestedOptions(
			dhcpv4.OptionSubnetMask,
			dhcpv4.OptionRouter,
			dhcpv4.OptionDomainName,
			dhcpv4.OptionDomainNameServer,
			dhcpv4.OptionNTPServers,
		),
	)
	if err != nil {
		return nil, fmt.Errorf("dhcp: building a renewal: %w", err)
	}

	msg.ClientIPAddr = addr.To4()

	msg.SetUnicast()

	return msg, nil
}

const renewWait = 10 * time.Second

// RFC 2131 4.4.5: retransmit no sooner than 60 s.
const retransmitFloor = 60 * time.Second

func retransmitAfter(left time.Duration) time.Duration {
	return min(max(left/2, retransmitFloor), left)
}

// RFC 2131 4.4.5: RENEWING unicasts to the granting server from T1; REBINDING broadcasts from T2.
func extend(ctx context.Context, iface string, lease *Lease, hostname string) (*Lease, error) {
	link, err := net.InterfaceByName(iface)
	if err != nil {
		return nil, fmt.Errorf("dhcp: %s: %w", iface, err)
	}

	for _, phase := range []struct {
		broadcasting bool
		until        time.Time
	}{
		{false, lease.Rebind},
		{true, lease.Expires},
	} {
		for time.Now().Before(phase.until) {
			if err := ctx.Err(); err != nil {
				return nil, err
			}

			got, err := ask(ctx, link, lease, phase.broadcasting, hostname)
			switch {
			case errors.Is(err, errRefused):
				return nil, err
			case err == nil:
				return got, nil
			}

			if err := sleep(ctx, retransmitAfter(time.Until(phase.until))); err != nil {
				return nil, err
			}
		}
	}
	return nil, fmt.Errorf("dhcp: %s could not be renewed before it expired", lease.Address.IP)
}

func ask(
	ctx context.Context,
	link *net.Interface,
	lease *Lease,
	broadcasting bool,
	hostname string,
) (*Lease, error) {
	msg, err := renewMessage(link.HardwareAddr, lease.Address.IP, broadcasting, hostname)
	if err != nil {
		return nil, err
	}

	to := lease.Server
	if broadcasting {
		to = net.IPv4bcast
	}

	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: lease.Address.IP, Port: clientPort})
	if err != nil {
		return nil, fmt.Errorf("dhcp: renewing to %s: %w", to, err)
	}
	defer conn.Close()

	return exchange(ctx, conn, &net.UDPAddr{IP: to, Port: serverPort}, msg, lease.Address.IP)
}

// A rebinding reply may come from any server.
func exchange(
	ctx context.Context,
	conn *net.UDPConn,
	to *net.UDPAddr,
	msg *dhcpv4.DHCPv4,
	addr net.IP,
) (*Lease, error) {
	deadline, ok := ctx.Deadline()
	if soon := time.Now().Add(renewWait); !ok || soon.Before(deadline) {
		deadline = soon
	}
	if err := conn.SetDeadline(deadline); err != nil {
		return nil, err
	}

	if _, err := conn.WriteToUDP(msg.ToBytes(), to); err != nil {
		return nil, fmt.Errorf("dhcp: renewing %s: %w", addr, err)
	}

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFromUDP(buf)
		if err != nil {
			return nil, fmt.Errorf("dhcp: waiting for a renewal of %s: %w", addr, err)
		}

		reply, err := dhcpv4.FromBytes(buf[:n])
		if err != nil || reply.TransactionID != msg.TransactionID {
			continue
		}

		switch reply.MessageType() {
		case dhcpv4.MessageTypeNak:
			return nil, errRefused
		case dhcpv4.MessageTypeAck:
			return leaseFrom(reply)
		}
	}
}
