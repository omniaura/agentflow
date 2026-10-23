package tools

import (
	"encoding/json"
	"fmt"
	"reflect"
	"testing"
)

func TestRefundTool(t *testing.T) {
	args, err := OfferRefundTool.Decode(`{"order_id":"A-1","reason":"damaged","lines":[{"sku":"mug","quantity":2}]}`)
	if err != nil {
		t.Fatal(err)
	}
	want := OfferRefundArgs{OrderId: "A-1", Reason: OfferRefundReasonDamaged, Lines: []OfferRefundArgsLines{{Sku: "mug", Quantity: 2}}}
	if !reflect.DeepEqual(args, want) {
		t.Fatalf("got %+v, want %+v", args, want)
	}

	_, err = OfferRefundTool.Decode(`{"order_id":"A-1","reason":"changed my mind"}`)
	if want := `offer_refund: invalid arguments: reason: "changed my mind" is not one of damaged, late, wrong_item, other`; err == nil || err.Error() != want {
		t.Fatalf("got %v, want %s", err, want)
	}
}

func TestToolSchemas(t *testing.T) {
	for _, schema := range [][]byte{LookupOrderTool.Schema, OfferRefundTool.Schema} {
		var v map[string]any
		if err := json.Unmarshal(schema, &v); err != nil {
			t.Fatalf("%s: %v", schema, err)
		}
	}
	if LookupOrderTool.MaxBytes != 1024 {
		t.Fatalf("MaxBytes = %d", LookupOrderTool.MaxBytes)
	}
}

func ExampleSystem() {
	prompt := System{LookupTool: LookupOrderTool.Name, RefundTool: OfferRefundTool.Name}
	fmt.Println(prompt.String())
	// Output:
	// You are a support agent. Look orders up with lookup_order before answering
	// questions about them, and offer a refund with offer_refund only when the
	// order is eligible.
}
