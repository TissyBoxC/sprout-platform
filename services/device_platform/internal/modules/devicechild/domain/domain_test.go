package domain

import (
	"testing"
)

func TestRequestValidate(t *testing.T) {
	valid := Request{FamilyID: "f", DeviceID: "d", ChildID: "c"}
	if err := valid.Validate(); err != nil {
		t.Fatalf("valid request rejected: %v", err)
	}
	cases := map[string]Request{
		"missing family": {DeviceID: "d", ChildID: "c"},
		"missing device": {FamilyID: "f", ChildID: "c"},
		"missing child":  {FamilyID: "f", DeviceID: "d"},
	}
	for name, request := range cases {
		if err := request.Validate(); err == nil {
			t.Fatalf("%s: expected an error", name)
		}
	}
}

func TestResolveStatesAreDistinct(t *testing.T) {
	// The three states drive three different guardian messages, so they must not
	// collapse into one value.
	states := map[ResolveState]bool{
		ResolveAssigned:   true,
		ResolveNoChild:    true,
		ResolveUnassigned: true,
	}
	if len(states) != 3 {
		t.Fatalf("states are not distinct: %v", states)
	}
}
