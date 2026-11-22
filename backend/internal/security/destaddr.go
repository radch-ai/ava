package security

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
)

// AddrClass is the destination class of one resolved address.
type AddrClass string

const (
	// ClassPublic is an ordinary internet-routable address: the default
	// allowed destination.
	ClassPublic AddrClass = "public"
	// ClassLoopback is a 127/8 or ::1 address.
	ClassLoopback AddrClass = "loopback"
	// ClassPrivate is RFC1918/ULA space plus carrier-grade NAT.
	ClassPrivate AddrClass = "private"
	// ClassLinkLocal covers link-local unicast, multicast and the
	// metadata-service addresses: always denied, never overridable.
	ClassLinkLocal AddrClass = "linklocal"
	// ClassUnspecified is 0.0.0.0 or ::.
	ClassUnspecified AddrClass = "unspecified"
)

var (
	// ErrLinkLocalDenied: a link-local/metadata destination.
	ErrLinkLocalDenied = errors.New("destination denied: link-local or metadata address")
	// ErrUnspecifiedDenied: an unspecified destination.
	ErrUnspecifiedDenied = errors.New("destination denied: unspecified address")
	// ErrPrivateDenied: a loopback/LAN destination without its
	// administrator-approved exception.
	ErrPrivateDenied = errors.New("destination denied: non-public address")
	// ErrUnresolved: the host produced no address to validate.
	ErrUnresolved = errors.New("destination denied: host did not resolve")
)

// cgnat is RFC 6598 shared address space: not internet-routable, so it
// belongs with the private classes rather than the public default.
var cgnat = netip.MustParsePrefix("100.64.0.0/10")

// lookupNetIP resolves through the default resolver; a package-level seam
// so tests prove DNS shares the caller's deadline.
var lookupNetIP = net.DefaultResolver.LookupNetIP

// ResolveClass resolves host to every candidate address and its class.
// A literal address skips DNS entirely; a name resolves through the
// default resolver. IPv4-mapped IPv6 forms are normalized first so one
// destination cannot be classified two ways.
func ResolveClass(ctx context.Context, host string) ([]netip.Addr, []AddrClass, error) {
	if literal, err := netip.ParseAddr(host); err == nil {
		addr := literal.Unmap()
		return []netip.Addr{addr}, []AddrClass{ClassifyAddr(addr)}, nil
	}
	addrs, err := lookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: %s: %v", ErrUnresolved, host, err)
	}
	if len(addrs) == 0 {
		return nil, nil, fmt.Errorf("%w: %s", ErrUnresolved, host)
	}
	out := make([]netip.Addr, 0, len(addrs))
	classes := make([]AddrClass, 0, len(addrs))
	for _, addr := range addrs {
		normalized := addr.Unmap()
		out = append(out, normalized)
		classes = append(classes, ClassifyAddr(normalized))
	}
	return out, classes, nil
}

// ClassifyAddr maps one address to its class. Multicast shares
// link-local's always-denied treatment; nothing here is configurable.
func ClassifyAddr(addr netip.Addr) AddrClass {
	switch {
	case addr.IsUnspecified():
		return ClassUnspecified
	case addr.IsLoopback():
		return ClassLoopback
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(), addr.IsMulticast():
		return ClassLinkLocal
	case addr.IsPrivate(), cgnat.Contains(addr):
		return ClassPrivate
	default:
		return ClassPublic
	}
}

// DestinationAllowed applies the destination policy to one host's
// classes: link-local/unspecified/metadata destinations are denied
// unconditionally; otherwise every candidate must be public, or — with
// the server's approved exception — in {public, loopback, private}.
// Mixed answers are governed as a whole: no candidate may be outside
// the permitted scope.
func DestinationAllowed(classes []AddrClass, allowPrivate bool) error {
	if len(classes) == 0 {
		return ErrUnresolved
	}
	for _, class := range classes {
		switch class {
		case ClassLinkLocal:
			return ErrLinkLocalDenied
		case ClassUnspecified:
			return ErrUnspecifiedDenied
		}
	}
	if allowPrivate {
		for _, class := range classes {
			if class != ClassPublic && class != ClassLoopback && class != ClassPrivate {
				return ErrPrivateDenied
			}
		}
		return nil
	}
	for _, class := range classes {
		if class != ClassPublic {
			return ErrPrivateDenied
		}
	}
	return nil
}
