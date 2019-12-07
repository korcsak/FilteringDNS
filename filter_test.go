package filter

import (
	"context"
	"testing"

	"github.com/caddyserver/caddy"
	"github.com/coredns/coredns/plugin/pkg/dnstest"
	"github.com/coredns/coredns/plugin/test"

	"github.com/miekg/dns"
)

func Test_ServeDNS(t *testing.T) {
	c := caddy.NewTestController("dns", `filter  {
		allow https://dl.paesacybersecurity.eu/lists/whitelist.txt
		block https://dl.paesacybersecurity.eu/lists/blacklist.txt
	}`)

	f, err := parseConfig(c)
	if err != nil {
		t.Fatal(err)
	}
	f.Next = test.ErrorHandler()

	if err = f.Load(); err != nil {
		t.Fatal(err)
	}

	rec := dnstest.NewRecorder(&test.ResponseWriter{})
	req := new(dns.Msg)
	req.SetQuestion("ads.example.org", dns.TypeA)

	_, err = f.ServeDNS(context.TODO(), rec, req)
	if err != nil {
		t.Fatal(err)
	}
}
