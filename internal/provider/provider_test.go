package provider

import (
	"context"
	"testing"

	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	fwresource "github.com/hashicorp/terraform-plugin-framework/resource"
)

func TestProviderSchema_Valid(t *testing.T) {
	ctx := context.Background()
	p := New("test")()

	var resp fwprovider.SchemaResponse
	p.Schema(ctx, fwprovider.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building provider schema: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("provider schema failed validation: %v", diags)
	}
}

func TestVMResourceSchema_Valid(t *testing.T) {
	ctx := context.Background()
	r := NewVMResource()

	var resp fwresource.SchemaResponse
	r.Schema(ctx, fwresource.SchemaRequest{}, &resp)
	if resp.Diagnostics.HasError() {
		t.Fatalf("unexpected diagnostics building vm resource schema: %v", resp.Diagnostics)
	}
	if diags := resp.Schema.ValidateImplementation(ctx); diags.HasError() {
		t.Fatalf("vm resource schema failed validation: %v", diags)
	}
}

func TestProviderResources_Registered(t *testing.T) {
	p := New("test")()
	resources := p.(interface {
		Resources(context.Context) []func() fwresource.Resource
	}).Resources(context.Background())

	if len(resources) != 1 {
		t.Fatalf("expected exactly 1 resource registered, got %d", len(resources))
	}
	if _, ok := resources[0]().(*vmResource); !ok {
		t.Fatalf("expected the registered resource to be *vmResource")
	}
}
