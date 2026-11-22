package security

import (
	"context"
	"errors"
	"net/netip"
	"testing"
	"time"
)

// Every address family lands in exactly one class, IPv4-mapped IPv6
// included.
func TestClassifyAddr(t *testing.T) {
	cases := map[string]AddrClass{
		"93.184.216.34":                      ClassPublic,
		"2606:2800:220:1:248:1893:25c8:1946": ClassPublic,
		"127.0.0.1":                          ClassLoopback,
		"::1":                                ClassLoopback,
		"10.0.0.5":                           ClassPrivate,
		"172.16.4.4":                         ClassPrivate,
		"192.168.1.50":                       ClassPrivate,
		"fd00::1":                            ClassPrivate,
		"100.64.0.1":                         ClassPrivate, // CGNAT is not public
		"169.254.169.254":                    ClassLinkLocal,
		"fe80::1":                            ClassLinkLocal,
		"224.0.0.1":                          ClassLinkLocal,
		"0.0.0.0":                            ClassUnspecified,
		"::":                                 ClassUnspecified,
		"::ffff:192.168.1.50":                ClassPrivate, // IPv4-mapped
		"::ffff:169.254.169.254":             ClassLinkLocal,
	}
	for raw, want := range cases {
		addr := netip.MustParseAddr(raw)
		if got := ClassifyAddr(addr); got != want {
			t.Errorf("ClassifyAddr(%s) = %s, want %s", raw, got, want)
		}
	}
}

// The policy matrix: public is the default; loopback/LAN needs the
// server's exception; link-local, metadata and unspecified are denied
// whatever the exception says.
func TestDestinationAllowed(t *testing.T) {
	cases := []struct {
		name         string
		classes      []AddrClass
		allowPrivate bool
		want         error
	}{
		{"public", []AddrClass{ClassPublic}, false, nil},
		{"public with exception", []AddrClass{ClassPublic}, true, nil},
		{"loopback default", []AddrClass{ClassLoopback}, false, ErrPrivateDenied},
		{"loopback approved", []AddrClass{ClassLoopback}, true, nil},
		{"private default", []AddrClass{ClassPrivate}, false, ErrPrivateDenied},
		{"private approved", []AddrClass{ClassPrivate}, true, nil},
		{"mixed default", []AddrClass{ClassPublic, ClassPrivate}, false, ErrPrivateDenied},
		{"mixed approved", []AddrClass{ClassPublic, ClassPrivate}, true, nil},
		{"link-local always", []AddrClass{ClassLinkLocal}, true, ErrLinkLocalDenied},
		{"link-local mixed", []AddrClass{ClassPublic, ClassLinkLocal}, true, ErrLinkLocalDenied},
		{"unspecified always", []AddrClass{ClassUnspecified}, true, ErrUnspecifiedDenied},
		{"no candidates", nil, true, ErrUnresolved},
	}
	for _, tc := range cases {
		err := DestinationAllowed(tc.classes, tc.allowPrivate)
		if tc.want == nil {
			if err != nil {
				t.Errorf("%s: err = %v, want allowed", tc.name, err)
			}
			continue
		}
		if !errors.Is(err, tc.want) {
			t.Errorf("%s: err = %v, want %v", tc.name, err, tc.want)
		}
	}
}

// Literal hosts resolve without DNS, including the IPv4-mapped form; a
// name that cannot resolve is a denial, not a pass-through.
func TestResolveClass(t *testing.T) {
	addrs, classes, err := ResolveClass(context.Background(), "169.254.169.254")
	if err != nil {
		t.Fatalf("literal resolve: %v", err)
	}
	if len(addrs) != 1 || classes[0] != ClassLinkLocal {
		t.Fatalf("literal resolve = %v %v, want one linklocal address", addrs, classes)
	}

	addrs, classes, err = ResolveClass(context.Background(), "::ffff:127.0.0.1")
	if err != nil {
		t.Fatalf("mapped resolve: %v", err)
	}
	if len(addrs) != 1 || !addrs[0].Is4() || classes[0] != ClassLoopback {
		t.Fatalf("mapped resolve = %v %v, want one v4 loopback", addrs, classes)
	}

	if _, _, err := ResolveClass(context.Background(), "localhost"); err != nil {
		t.Fatalf("localhost resolve: %v", err)
	}
	if _, _, err := ResolveClass(context.Background(), "does-not-exist.invalid"); !errors.Is(err, ErrUnresolved) {
		t.Fatalf("unresolvable name err = %v, want ErrUnresolved", err)
	}
}

// DNS resolution shares the caller's deadline — a resolver that
// never answers is bounded by the context, not by its own goodwill.
func TestResolveClassRespectsDeadline(t *testing.T) {
	orig := lookupNetIP
	t.Cleanup(func() { lookupNetIP = orig })
	lookupNetIP = func(ctx context.Context, network, host string) ([]netip.Addr, error) {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	_, _, err := ResolveClass(ctx, "never-resolves.example")
	if !errors.Is(err, ErrUnresolved) {
		t.Fatalf("err = %v, want ErrUnresolved", err)
	}
	if elapsed := time.Since(start); elapsed > 5*time.Second {
		t.Fatalf("resolve took %v, want it bounded near the 100ms deadline", elapsed)
	}
}
