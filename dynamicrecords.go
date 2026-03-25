package dynamicrecords

import (
	"context"

	"github.com/coredns/coredns/plugin"
	"github.com/coredns/coredns/request"
	"github.com/miekg/dns"
)

// DynamicRecords is a CoreDNS plugin that serves records from an in-memory buffer
type DynamicRecords struct {
	Next         plugin.Handler
	sharedServer *SharedServer
}

// Name returns the plugin name
func (dr *DynamicRecords) Name() string {
	return "dynamicrecords"
}

// ServeDNS implements the plugin.Handler interface
func (dr *DynamicRecords) ServeDNS(ctx context.Context, w dns.ResponseWriter, r *dns.Msg) (int, error) {
	state := request.Request{W: w, Req: r}

	// Create a response writer wrapper to capture downstream responses
	nw := &nonWriter{ResponseWriter: w, msg: new(dns.Msg)}

	// Call downstream plugins
	rcode, err := plugin.NextOrFailure(dr.Name(), dr.Next, ctx, nw, r)

	// Get the response message from the nonWriter
	msg := nw.msg

	// Only process if downstream returned NODATA (rcode 0 with no answers) or NXDOMAIN (rcode 3)
	shouldAppend := false
	if msg != nil {
		if (msg.Rcode == dns.RcodeSuccess && len(msg.Answer) == 0) || msg.Rcode == dns.RcodeNameError {
			shouldAppend = true
		}
	}

	if shouldAppend && len(r.Question) > 0 {
		q := r.Question[0]

		// Try to get records from shared buffer
		records := dr.sharedServer.buffer.Get(q.Name, q.Qtype)

		if len(records) > 0 {
			// We have records to append, set rcode to success
			msg.Rcode = dns.RcodeSuccess
			msg.Answer = append(msg.Answer, records...)

			// Ensure the response has the correct flags
			msg.Response = true
			msg.Authoritative = false
			msg.RecursionAvailable = true

			// Write the modified response
			w.WriteMsg(msg)
			return dns.RcodeSuccess, err
		}
	}

	// If we didn't append anything, or if we shouldn't modify the response,
	// pass through the original response
	if msg != nil && nw.written {
		// Response was already written by downstream or our logic above
		return rcode, err
	}

	// Write the unmodified message if it hasn't been written yet
	if msg != nil {
		w.WriteMsg(msg)
	}

	return rcode, err
}

// nonWriter is a ResponseWriter that captures the response without writing it
type nonWriter struct {
	dns.ResponseWriter
	msg     *dns.Msg
	written bool
}

func (n *nonWriter) WriteMsg(m *dns.Msg) error {
	n.msg = m.Copy()
	n.written = true
	// Don't actually write to the underlying writer yet
	return nil
}

func (n *nonWriter) Write(b []byte) (int, error) {
	// Parse the message from bytes if WriteMsg wasn't called
	if n.msg == nil {
		n.msg = new(dns.Msg)
		if err := n.msg.Unpack(b); err != nil {
			return 0, err
		}
	}
	n.written = true
	return len(b), nil
}
