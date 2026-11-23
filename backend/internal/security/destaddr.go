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
	// ClassReserved covers IETF-special space that is not
	// internet-routable and not administratively approvable: the "this
	// network" block, benchmarking, documentation and transition
	// ranges, reserved IPv4 space and the discard prefix. Always
	// denied, like link-local.
	ClassReserved AddrClass = "reserved"
	// ClassUnspecified is 0.0.0.0 or ::.
	ClassUnspecified AddrClass = "unspecified"
)

var (
	// ErrLinkLocalDenied: a link-local/metadata destination.
	ErrLinkLocalDenied = errors.New("destination denied: link-local or metadata address")
	// ErrReservedDenied: a reserved/special-purpose destination.
	ErrReservedDenied = errors.New("destination denied: reserved address")
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

// reservedRanges is IETF-special space that must never classify as
// public: nothing routable lives here, so an answer inside any range is
// denied outright, exactly like link-local — no approval widens it.
// 0.0.0.0/8 ("this network"), 192.0.0.0/24 (IETF protocol assignments),
// 198.18.0.0/15 (benchmarking), 240.0.0.0/4 + 255.255.255.255
// (reserved/broadcast), 64:ff9b::/96 (NAT64 well-known prefix),
// 2002::/16 (6to4), 2001:db8::/32 (documentation), 100::/64 (discard).
var reservedRanges = []netip.Prefix{
	netip.MustParsePrefix("0.0.0.0/8"),
	netip.MustParsePrefix("192.0.0.0/24"),
	netip.MustParsePrefix("198.18.0.0/15"),
	netip.MustParsePrefix("240.0.0.0/4"),
	netip.MustParsePrefix("255.255.255.255/32"),
	netip.MustParsePrefix("64:ff9b::/96"),
	netip.MustParsePrefix("2002::/16"),
	netip.MustParsePrefix("2001:db8::/32"),
	netip.MustParsePrefix("100::/64"),
}

// isReserved reports whether addr falls inside any reserved range.
func isReserved(addr netip.Addr) bool {
	for _, prefix := range reservedRanges {
		if prefix.Contains(addr) {
			return true
		}
	}
	return false
}

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

// ClassifyAddr maps one address to its class. Multicast and IETF
// reserved ranges share link-local's always-denied treatment; an
// invalid (zero) address can never name a real destination, so it
// classifies reserved rather than falling through to public. Nothing
// here is configurable.
func ClassifyAddr(addr netip.Addr) AddrClass {
	switch {
	case !addr.IsValid():
		return ClassReserved
	case addr.IsUnspecified():
		return ClassUnspecified
	case addr.IsLoopback():
		return ClassLoopback
	case addr.IsLinkLocalUnicast(), addr.IsLinkLocalMulticast(), addr.IsMulticast():
		return ClassLinkLocal
	case isReserved(addr):
		return ClassReserved
	case addr.IsPrivate(), cgnat.Contains(addr):
		return ClassPrivate
	default:
		return ClassPublic
	}
}

// DestinationAllowed applies the destination policy to one host's
// classes: link-local, reserved and unspecified destinations are denied
// unconditionally — no approval widens them; otherwise every candidate
// must be public, or — with the server's approved exception — in
// {public, loopback, private}. Mixed answers are governed as a whole:
// no candidate may be outside the permitted scope.
func DestinationAllowed(classes []AddrClass, allowPrivate bool) error {
	if len(classes) == 0 {
		return ErrUnresolved
	}
	for _, class := range classes {
		switch class {
		case ClassLinkLocal:
			return ErrLinkLocalDenied
		case ClassReserved:
			return ErrReservedDenied
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
