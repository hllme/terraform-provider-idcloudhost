package client

import (
	"context"
	"net/url"
	"strconv"
)

const vmPath = "/v1/user-resource/vm"

// VM is the API's representation of a virtual machine, per
// PROVIDER_DESIGN.md §3.1.
type VM struct {
	UUID               string `json:"uuid"`
	Name               string `json:"name"`
	Status             string `json:"status"`
	BillingAccountID   int    `json:"billing_account_id"`
	Location           string `json:"location"`
	OSName             string `json:"os_name"`
	OSVersion          string `json:"os_version"`
	VCPU               int    `json:"vcpu"`
	RAM                int    `json:"ram"`
	Disks              int    `json:"disks"`
	Username           string `json:"username"`
	PrivateNetworkUUID string `json:"private_network_uuid,omitempty"`
	FloatIPAddress     string `json:"float_ip_address,omitempty"`
	ReservePublicIP    bool   `json:"reserve_public_ip"`
	Backup             bool   `json:"backup"`
}

// CreateVMInput is the request body for creating a VM.
type CreateVMInput struct {
	Name               string `json:"name"`
	BillingAccountID   int    `json:"billing_account_id"`
	Location           string `json:"location,omitempty"`
	OSName             string `json:"os_name"`
	OSVersion          string `json:"os_version"`
	VCPU               int    `json:"vcpu"`
	RAM                int    `json:"ram"`
	Disks              int    `json:"disks"`
	Username           string `json:"username"`
	Password           string `json:"password"`
	PublicKey          string `json:"public_key,omitempty"`
	CloudInit          string `json:"cloud_init,omitempty"`
	PrivateNetworkUUID string `json:"private_network_uuid,omitempty"`
	ReservePublicIP    bool   `json:"reserve_public_ip"`
	Backup             bool   `json:"backup"`
}

// CreateVM issues the create call. It does not wait for the VM to become
// ready — callers poll GetVM for that.
func (c *Client) CreateVM(ctx context.Context, in CreateVMInput) (*VM, error) {
	form := url.Values{}
	form.Set("name", in.Name)
	form.Set("billing_account_id", strconv.Itoa(in.BillingAccountID))
	if in.Location != "" {
		form.Set("location", in.Location)
	}
	form.Set("os_name", in.OSName)
	form.Set("os_version", in.OSVersion)
	form.Set("vcpu", strconv.Itoa(in.VCPU))
	form.Set("ram", strconv.Itoa(in.RAM))
	form.Set("disks", strconv.Itoa(in.Disks))
	form.Set("username", in.Username)
	form.Set("password", in.Password)
	if in.PublicKey != "" {
		form.Set("public_key", in.PublicKey)
	}
	if in.CloudInit != "" {
		form.Set("cloud_init", in.CloudInit)
	}
	if in.PrivateNetworkUUID != "" {
		form.Set("private_network_uuid", in.PrivateNetworkUUID)
	}
	form.Set("reserve_public_ip", strconv.FormatBool(in.ReservePublicIP))
	form.Set("backup", strconv.FormatBool(in.Backup))

	var out VM
	if err := c.doForm(ctx, "POST", vmPath, form, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// GetVM reads a VM by UUID. Callers should use client.NotFound(err) to
// detect a 404 and remove the resource from state.
//
// This also serves as the provider's one smoke-test method: a bare
// `GetVM` call is enough to confirm the apikey and base URL are wired up
// correctly.
func (c *Client) GetVM(ctx context.Context, uuid string) (*VM, error) {
	var out VM
	path := vmPath + "?uuid=" + url.QueryEscape(uuid)
	if err := c.doForm(ctx, "GET", path, nil, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

// DeleteVM issues the delete call. It does not wait for the VM to be
// gone — callers poll GetVM (expecting NotFound) for that.
func (c *Client) DeleteVM(ctx context.Context, uuid string) error {
	path := vmPath + "?uuid=" + url.QueryEscape(uuid)
	return c.doForm(ctx, "DELETE", path, nil, nil)
}
