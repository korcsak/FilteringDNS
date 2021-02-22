package filter

import (
	"context"
	"strings"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/plugin/metrics"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

const defaultResponseTTL = 3600 // Default TTL used for generated responses.

// Filter represents a plugin instance that can filter and block requests based
// on predefined lists and regex rules.
type Filter struct {
	Next plugin.Handler

	allowlist *matcher
	denylist  *matcher

	// sources to load data into filters.
	sources []source

	// uncloak enables response inspection for CNAME cloaking.
	uncloak bool

	// ttl sets a custom TTL.
	ttl uint32
}

func New() *Filter {
	return &Filter{
		allowlist: newMatcher(),
		denylist:  newMatcher(),
		ttl:       defaultResponseTTL,
	}
}

// ServeDNS implements the plugin.Handler interface.
func (f *Filter) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}
	server := metrics.WithServer(ctx)
	qname := state.Name()

	if f.Match(qname) {
		BlockCount.WithLabelValues(server).Inc()

		msg := createReply(r, f.ttl)
		w.WriteMsg(msg) //nolint
		return dns.RcodeSuccess, nil
	}

	if f.uncloak {
		rw := &ResponseWriter{ResponseWriter: w, state: state, server: server, Filter: f}
		return plugin.NextOrFailure(f.Name(), f.Next, ctx, rw, r)
	}

	return plugin.NextOrFailure(f.Name(), f.Next, ctx, w, r)
}

// Name implements the plugin.Handler interface.
func (f *Filter) Name() string {
	return "filter"
}

// Match determines if the requested name should be blocked or allowed.
func (f *Filter) Match(name string) bool {
	if f.allowlist.Match(name) {
		return false
	}
	if f.denylist.Match(name) {
		return true
	}
	return false
}

func (f *Filter) Load() error {
	for _, list := range f.sources {
		rc, err := list.Read()
		if err != nil {
			return err
		}
		defer rc.Close()

		if list.Block {
			if _, err := f.denylist.Load(rc); err != nil {
				return err
			}
		} else {
			if _, err := f.allowlist.Load(rc); err != nil {
				return err
			}
		}
	}

	return nil
}

// ResponseWriter is a response writer that performs response uncloaking.
type ResponseWriter struct {
	dns.ResponseWriter
	*Filter

	state  request.Request
	server string
}

// WriteMsg implements the dns.ResponseWriter interface.
func (w *ResponseWriter) WriteMsg(m *dns.Msg) error {
	if m.Rcode != dns.RcodeSuccess {
		return w.ResponseWriter.WriteMsg(m)
	}

	if w.allowlist.Match(w.state.Name()) {
		return w.ResponseWriter.WriteMsg(m)
	}

	for _, r := range m.Answer {
		header := r.Header()
		if header.Class != dns.ClassINET {
			continue
		}

		var target string
		switch header.Rrtype {
		case dns.TypeCNAME:
			target = r.(*dns.CNAME).Target
		case dns.TypeSVCB:
			target = r.(*dns.SVCB).Target
		case dns.TypeHTTPS:
			target = r.(*dns.HTTPS).Target
		default:
			continue
		}

		target = strings.TrimSuffix(target, ".")
		if w.Match(target) {
			BlockCount.WithLabelValues(w.server).Inc()

			r := w.state.Req
			msg := createReply(r, w.ttl)
			w.WriteMsg(msg) //nolint
			return nil
		}
	}
	return w.ResponseWriter.WriteMsg(m)
}

func createReply(r *dns.Msg, ttl uint32) *dns.Msg {
	state := request.Request{Req: r}

	switch state.QType() {
	case dns.TypeA:
		return newAResponse(r, ttl)

	case dns.TypeAAAA:
		return newAAAAResponse(r, ttl)

	default:
		return newNXDomainResponse(r, ttl)
	}
}
