package dynamicrecords

import (
	"testing"
	"time"

	"github.com/miekg/dns"
)

func makeA(t *testing.T, rr string) dns.RR {
	t.Helper()
	r, err := dns.NewRR(rr)
	if err != nil {
		t.Fatalf("dns.NewRR(%q): %v", rr, err)
	}
	return r
}

func TestRRBuffer_AddAndGet(t *testing.T) {
	b := NewRRBuffer()
	rr := makeA(t, "example.com. 300 IN A 1.2.3.4")
	expiry := time.Now().Add(1 * time.Hour)

	b.Add("example.com.", dns.TypeA, []dns.RR{rr}, expiry)

	got := b.Get("example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Get: got %d records, want 1", len(got))
	}
}

func TestRRBuffer_CaseInsensitive(t *testing.T) {
	b := NewRRBuffer()
	rr := makeA(t, "Example.COM. 300 IN A 1.2.3.4")
	b.Add("Example.COM.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour))

	// Lookup with different case should still find the record.
	got := b.Get("example.com.", dns.TypeA)
	if len(got) == 0 {
		t.Error("Get: record not found with normalised qname")
	}
}

func TestRRBuffer_MissOnWrongType(t *testing.T) {
	b := NewRRBuffer()
	rr := makeA(t, "example.com. 300 IN A 1.2.3.4")
	b.Add("example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour))

	if got := b.Get("example.com.", dns.TypeAAAA); got != nil {
		t.Errorf("Get(AAAA): expected nil, got %v", got)
	}
}

func TestRRBuffer_ExpiredNotReturned(t *testing.T) {
	b := NewRRBuffer()
	rr := makeA(t, "exp.example.com. 300 IN A 1.2.3.4")
	// Set expiry in the past.
	b.Add("exp.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(-1*time.Second))

	if got := b.Get("exp.example.com.", dns.TypeA); got != nil {
		t.Errorf("Get: expected nil for expired record, got %v", got)
	}
}

func TestRRBuffer_AddOverwrites(t *testing.T) {
	b := NewRRBuffer()
	rr1 := makeA(t, "ow.example.com. 300 IN A 1.1.1.1")
	rr2 := makeA(t, "ow.example.com. 300 IN A 2.2.2.2")
	b.Add("ow.example.com.", dns.TypeA, []dns.RR{rr1}, time.Now().Add(time.Hour))
	b.Add("ow.example.com.", dns.TypeA, []dns.RR{rr2}, time.Now().Add(time.Hour))

	got := b.Get("ow.example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Get after overwrite: got %d records, want 1", len(got))
	}
	if got[0].(*dns.A).A.String() != "2.2.2.2" {
		t.Errorf("overwrite: got IP %s, want 2.2.2.2", got[0].(*dns.A).A)
	}
}

func TestRRBuffer_Delete(t *testing.T) {
	b := NewRRBuffer()
	rr := makeA(t, "del.example.com. 300 IN A 1.2.3.4")
	b.Add("del.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour))
	b.Delete("del.example.com.", dns.TypeA)

	if got := b.Get("del.example.com.", dns.TypeA); got != nil {
		t.Errorf("Get after Delete: expected nil, got %v", got)
	}
}

func TestRRBuffer_DeleteRecords_Partial(t *testing.T) {
	b := NewRRBuffer()
	rr1 := makeA(t, "multi.example.com. 300 IN A 10.0.0.1")
	rr2 := makeA(t, "multi.example.com. 300 IN A 10.0.0.2")
	b.Add("multi.example.com.", dns.TypeA, []dns.RR{rr1, rr2}, time.Now().Add(time.Hour))

	deleted := b.DeleteRecords("multi.example.com.", dns.TypeA, []dns.RR{rr1})
	if deleted != 1 {
		t.Errorf("DeleteRecords: deleted %d, want 1", deleted)
	}

	got := b.Get("multi.example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Get after partial delete: got %d records, want 1", len(got))
	}
	if got[0].(*dns.A).A.String() != "10.0.0.2" {
		t.Errorf("remaining record: got IP %s, want 10.0.0.2", got[0].(*dns.A).A)
	}
}

func TestRRBuffer_DeleteRecords_All(t *testing.T) {
	b := NewRRBuffer()
	rr := makeA(t, "all.example.com. 300 IN A 1.2.3.4")
	b.Add("all.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour))

	deleted := b.DeleteRecords("all.example.com.", dns.TypeA, []dns.RR{rr})
	if deleted != 1 {
		t.Errorf("DeleteRecords: deleted %d, want 1", deleted)
	}
	if got := b.Get("all.example.com.", dns.TypeA); got != nil {
		t.Errorf("Get after full delete: expected nil, got %v", got)
	}
}

func TestRRBuffer_DeleteRecords_NoMatch(t *testing.T) {
	b := NewRRBuffer()
	rr1 := makeA(t, "nm.example.com. 300 IN A 1.2.3.4")
	rr2 := makeA(t, "nm.example.com. 300 IN A 9.9.9.9")
	b.Add("nm.example.com.", dns.TypeA, []dns.RR{rr1}, time.Now().Add(time.Hour))

	deleted := b.DeleteRecords("nm.example.com.", dns.TypeA, []dns.RR{rr2})
	if deleted != 0 {
		t.Errorf("DeleteRecords no-match: deleted %d, want 0", deleted)
	}
	if got := b.Get("nm.example.com.", dns.TypeA); len(got) != 1 {
		t.Errorf("Get: expected 1 record to remain, got %d", len(got))
	}
}

func TestRRBuffer_Size(t *testing.T) {
	b := NewRRBuffer()
	if b.Size() != 0 {
		t.Errorf("initial Size: got %d, want 0", b.Size())
	}

	b.Add("a.example.com.", dns.TypeA, []dns.RR{makeA(t, "a.example.com. 60 IN A 1.1.1.1")}, time.Now().Add(time.Hour))
	b.Add("b.example.com.", dns.TypeA, []dns.RR{makeA(t, "b.example.com. 60 IN A 2.2.2.2")}, time.Now().Add(time.Hour))
	if b.Size() != 2 {
		t.Errorf("Size after 2 adds: got %d, want 2", b.Size())
	}

	b.Delete("a.example.com.", dns.TypeA)
	if b.Size() != 1 {
		t.Errorf("Size after delete: got %d, want 1", b.Size())
	}
}

func TestRRBuffer_GetReturnsCopy(t *testing.T) {
	b := NewRRBuffer()
	rr := makeA(t, "copy.example.com. 300 IN A 1.2.3.4")
	b.Add("copy.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour))

	got := b.Get("copy.example.com.", dns.TypeA)
	// Mutate the returned slice — should not affect the buffer.
	got[0].Header().Name = "modified."
	got2 := b.Get("copy.example.com.", dns.TypeA)
	if len(got2) == 0 {
		t.Error("buffer entry disappeared after external mutation")
	}
}
