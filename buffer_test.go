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
	b := NewRRBuffer(0)
	rr := makeA(t, "example.com. 300 IN A 1.2.3.4")
	expiry := time.Now().Add(1 * time.Hour)

	b.Add("example.com.", dns.TypeA, []dns.RR{rr}, expiry, false)

	got := b.Get("example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Get: got %d records, want 1", len(got))
	}
}

func TestRRBuffer_CaseInsensitive(t *testing.T) {
	b := NewRRBuffer(0)
	rr := makeA(t, "Example.COM. 300 IN A 1.2.3.4")
	b.Add("Example.COM.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)

	// Lookup with different case should still find the record.
	got := b.Get("example.com.", dns.TypeA)
	if len(got) == 0 {
		t.Error("Get: record not found with normalised qname")
	}
}

func TestRRBuffer_MissOnWrongType(t *testing.T) {
	b := NewRRBuffer(0)
	rr := makeA(t, "example.com. 300 IN A 1.2.3.4")
	b.Add("example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)

	if got := b.Get("example.com.", dns.TypeAAAA); got != nil {
		t.Errorf("Get(AAAA): expected nil, got %v", got)
	}
}

func TestRRBuffer_ExpiredNotReturned(t *testing.T) {
	b := NewRRBuffer(0)
	rr := makeA(t, "exp.example.com. 300 IN A 1.2.3.4")
	// Set expiry in the past.
	b.Add("exp.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(-1*time.Second), false)

	if got := b.Get("exp.example.com.", dns.TypeA); got != nil {
		t.Errorf("Get: expected nil for expired record, got %v", got)
	}
}

// TestRRBuffer_AddAppends verifies that successive Add calls accumulate batches.
func TestRRBuffer_AddAppends(t *testing.T) {
	b := NewRRBuffer(0)
	rr1 := makeA(t, "app.example.com. 300 IN A 1.1.1.1")
	rr2 := makeA(t, "app.example.com. 300 IN A 2.2.2.2")
	b.Add("app.example.com.", dns.TypeA, []dns.RR{rr1}, time.Now().Add(time.Hour), false)
	b.Add("app.example.com.", dns.TypeA, []dns.RR{rr2}, time.Now().Add(time.Hour), false)

	got := b.Get("app.example.com.", dns.TypeA)
	if len(got) != 2 {
		t.Fatalf("Get after two appends: got %d records, want 2", len(got))
	}
}

// TestRRBuffer_AddReplace verifies that replace=true discards existing batches.
func TestRRBuffer_AddReplace(t *testing.T) {
	b := NewRRBuffer(0)
	rr1 := makeA(t, "ow.example.com. 300 IN A 1.1.1.1")
	rr2 := makeA(t, "ow.example.com. 300 IN A 2.2.2.2")
	b.Add("ow.example.com.", dns.TypeA, []dns.RR{rr1}, time.Now().Add(time.Hour), false)
	b.Add("ow.example.com.", dns.TypeA, []dns.RR{rr2}, time.Now().Add(time.Hour), true) // replace

	got := b.Get("ow.example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Get after replace: got %d records, want 1", len(got))
	}
	if got[0].(*dns.A).A.String() != "2.2.2.2" {
		t.Errorf("replace: got IP %s, want 2.2.2.2", got[0].(*dns.A).A)
	}
}

// TestRRBuffer_GetAggregatesMultipleBatches verifies that Get merges all non-expired batches.
func TestRRBuffer_GetAggregatesMultipleBatches(t *testing.T) {
	b := NewRRBuffer(0)
	for _, ip := range []string{"10.0.0.1", "10.0.0.2", "10.0.0.3"} {
		rr := makeA(t, "agg.example.com. 300 IN A "+ip)
		b.Add("agg.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)
	}

	got := b.Get("agg.example.com.", dns.TypeA)
	if len(got) != 3 {
		t.Fatalf("Get: got %d records, want 3", len(got))
	}
}

func TestRRBuffer_Delete(t *testing.T) {
	b := NewRRBuffer(0)
	rr := makeA(t, "del.example.com. 300 IN A 1.2.3.4")
	b.Add("del.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)
	b.Delete("del.example.com.", dns.TypeA)

	if got := b.Get("del.example.com.", dns.TypeA); got != nil {
		t.Errorf("Get after Delete: expected nil, got %v", got)
	}
}

// TestRRBuffer_DeleteBatchExact removes a batch by exact record-set match.
func TestRRBuffer_DeleteBatchExact(t *testing.T) {
	b := NewRRBuffer(0)
	rr1 := makeA(t, "multi.example.com. 300 IN A 10.0.0.1")
	rr2 := makeA(t, "multi.example.com. 300 IN A 10.0.0.2")
	// Two separate batches
	b.Add("multi.example.com.", dns.TypeA, []dns.RR{rr1}, time.Now().Add(time.Hour), false)
	b.Add("multi.example.com.", dns.TypeA, []dns.RR{rr2}, time.Now().Add(time.Hour), false)

	deleted := b.DeleteRecords("multi.example.com.", dns.TypeA, []dns.RR{rr1})
	if deleted != 1 {
		t.Errorf("DeleteRecords: deleted %d records, want 1", deleted)
	}

	got := b.Get("multi.example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Get after batch delete: got %d records, want 1", len(got))
	}
	if got[0].(*dns.A).A.String() != "10.0.0.2" {
		t.Errorf("remaining record: got IP %s, want 10.0.0.2", got[0].(*dns.A).A)
	}
}

// TestRRBuffer_DeleteBatchOnlyOne verifies that deleting one of two identical
// batches removes exactly one, leaving the other intact.
func TestRRBuffer_DeleteBatchOnlyOne(t *testing.T) {
	b := NewRRBuffer(0)
	rr := makeA(t, "dup.example.com. 300 IN A 1.2.3.4")
	// Add the same record twice as two independent batches
	b.Add("dup.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)
	b.Add("dup.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)

	// First delete: removes batch 0
	deleted := b.DeleteRecords("dup.example.com.", dns.TypeA, []dns.RR{rr})
	if deleted != 1 {
		t.Errorf("first DeleteRecords: deleted %d, want 1", deleted)
	}
	got := b.Get("dup.example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("after first delete: got %d records, want 1", len(got))
	}

	// Second delete: removes batch 1
	deleted = b.DeleteRecords("dup.example.com.", dns.TypeA, []dns.RR{rr})
	if deleted != 1 {
		t.Errorf("second DeleteRecords: deleted %d, want 1", deleted)
	}
	if got = b.Get("dup.example.com.", dns.TypeA); got != nil {
		t.Errorf("after second delete: expected nil, got %v", got)
	}
}

// TestRRBuffer_DeleteBatchNoMatch verifies that a non-matching delete is a no-op.
func TestRRBuffer_DeleteBatchNoMatch(t *testing.T) {
	b := NewRRBuffer(0)
	rr1 := makeA(t, "nm.example.com. 300 IN A 1.2.3.4")
	rr2 := makeA(t, "nm.example.com. 300 IN A 9.9.9.9")
	b.Add("nm.example.com.", dns.TypeA, []dns.RR{rr1}, time.Now().Add(time.Hour), false)

	deleted := b.DeleteRecords("nm.example.com.", dns.TypeA, []dns.RR{rr2})
	if deleted != 0 {
		t.Errorf("DeleteRecords no-match: deleted %d, want 0", deleted)
	}
	if got := b.Get("nm.example.com.", dns.TypeA); len(got) != 1 {
		t.Errorf("Get: expected 1 record to remain, got %d", len(got))
	}
}

// TestRRBuffer_DeleteBatchAll verifies that deleting the only batch leaves nothing.
func TestRRBuffer_DeleteBatchAll(t *testing.T) {
	b := NewRRBuffer(0)
	rr := makeA(t, "all.example.com. 300 IN A 1.2.3.4")
	b.Add("all.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)

	deleted := b.DeleteRecords("all.example.com.", dns.TypeA, []dns.RR{rr})
	if deleted != 1 {
		t.Errorf("DeleteRecords: deleted %d, want 1", deleted)
	}
	if got := b.Get("all.example.com.", dns.TypeA); got != nil {
		t.Errorf("Get after full delete: expected nil, got %v", got)
	}
}

// TestRRBuffer_ExpiredBatchIndependent verifies that an expired batch is skipped
// while a live batch in the same (qname, qtype) is still returned.
func TestRRBuffer_ExpiredBatchIndependent(t *testing.T) {
	b := NewRRBuffer(0)
	expired := makeA(t, "exp2.example.com. 300 IN A 10.0.0.1")
	alive := makeA(t, "exp2.example.com. 300 IN A 10.0.0.2")

	b.Add("exp2.example.com.", dns.TypeA, []dns.RR{expired}, time.Now().Add(-time.Second), false) // expired
	b.Add("exp2.example.com.", dns.TypeA, []dns.RR{alive}, time.Now().Add(time.Hour), false)      // alive

	got := b.Get("exp2.example.com.", dns.TypeA)
	if len(got) != 1 {
		t.Fatalf("Get: got %d records, want 1 (only alive batch)", len(got))
	}
	if got[0].(*dns.A).A.String() != "10.0.0.2" {
		t.Errorf("alive record: got IP %s, want 10.0.0.2", got[0].(*dns.A).A)
	}
}

func TestRRBuffer_Size(t *testing.T) {
	b := NewRRBuffer(0)
	if b.Size() != 0 {
		t.Errorf("initial Size: got %d, want 0", b.Size())
	}

	b.Add("a.example.com.", dns.TypeA, []dns.RR{makeA(t, "a.example.com. 60 IN A 1.1.1.1")}, time.Now().Add(time.Hour), false)
	b.Add("b.example.com.", dns.TypeA, []dns.RR{makeA(t, "b.example.com. 60 IN A 2.2.2.2")}, time.Now().Add(time.Hour), false)
	if b.Size() != 2 {
		t.Errorf("Size after 2 adds: got %d, want 2", b.Size())
	}

	// A third add to the same qname+qtype appends a new batch
	b.Add("a.example.com.", dns.TypeA, []dns.RR{makeA(t, "a.example.com. 60 IN A 3.3.3.3")}, time.Now().Add(time.Hour), false)
	if b.Size() != 3 {
		t.Errorf("Size after append: got %d, want 3", b.Size())
	}

	b.Delete("a.example.com.", dns.TypeA)
	if b.Size() != 1 {
		t.Errorf("Size after delete: got %d, want 1", b.Size())
	}
}

func TestRRBuffer_GetReturnsCopy(t *testing.T) {
	b := NewRRBuffer(0)
	rr := makeA(t, "copy.example.com. 300 IN A 1.2.3.4")
	b.Add("copy.example.com.", dns.TypeA, []dns.RR{rr}, time.Now().Add(time.Hour), false)

	got := b.Get("copy.example.com.", dns.TypeA)
	// Mutate the returned slice — should not affect the buffer.
	got[0].Header().Name = "modified."
	got2 := b.Get("copy.example.com.", dns.TypeA)
	if len(got2) == 0 {
		t.Error("buffer entry disappeared after external mutation")
	}
}
