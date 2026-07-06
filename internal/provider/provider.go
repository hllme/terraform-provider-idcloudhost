package provider

import (
	"context"
	"os"

	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/provider/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/hllme/terraform-provider-idcloudhost/internal/client"
)

// Ensure IDCloudHostProvider satisfies the provider.Provider interface.
var _ provider.Provider = &IDCloudHostProvider{}

// IDCloudHostProvider is the provider implementation, per
// PROVIDER_DESIGN.md §2.
type IDCloudHostProvider struct {
	// version is set by main.go from the release build; "dev" locally.
	version string
}

// idcloudhostProviderModel maps the provider schema to Go types.
type idcloudhostProviderModel struct {
	APIKey          types.String `tfsdk:"apikey"`
	DefaultLocation types.String `tfsdk:"default_location"`
}

// ProviderData is what Configure hands to resources/data sources via
// req.ProviderData.
type ProviderData struct {
	Client          *client.Client
	DefaultLocation string
}

func New(version string) func() provider.Provider {
	return func() provider.Provider {
		return &IDCloudHostProvider{version: version}
	}
}

func (p *IDCloudHostProvider) Metadata(ctx context.Context, req provider.MetadataRequest, resp *provider.MetadataResponse) {
	resp.TypeName = "idcloudhost"
	resp.Version = p.version
}

func (p *IDCloudHostProvider) Schema(ctx context.Context, req provider.SchemaRequest, resp *provider.SchemaResponse) {
	resp.Schema = schema.Schema{
		Description: "Custom, project-scoped provider for IDCloudHost. In development; not published to the Terraform Registry.",
		Attributes: map[string]schema.Attribute{
			"apikey": schema.StringAttribute{
				Optional:    true,
				Sensitive:   true,
				Description: "IDCloudHost API key, sent as the `apikey` header on every request. Falls back to the IDCLOUDHOST_API_KEY environment variable.",
			},
			"default_location": schema.StringAttribute{
				Optional:    true,
				Description: "Fallback location (e.g. sgp01, jkt01, jkt02, jkt03) used when a resource omits its own `location`.",
			},
		},
	}
}

func (p *IDCloudHostProvider) Configure(ctx context.Context, req provider.ConfigureRequest, resp *provider.ConfigureResponse) {
	var data idcloudhostProviderModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	apiKey := os.Getenv("IDCLOUDHOST_API_KEY")
	if !data.APIKey.IsNull() && data.APIKey.ValueString() != "" {
		apiKey = data.APIKey.ValueString()
	}
	if apiKey == "" {
		resp.Diagnostics.AddAttributeError(
			path.Root("apikey"),
			"Missing API Key",
			"The provider requires an IDCloudHost API key. Set the `apikey` attribute or the IDCLOUDHOST_API_KEY environment variable.",
		)
		return
	}

	defaultLocation := data.DefaultLocation.ValueString()

	providerData := &ProviderData{
		Client:          client.New(apiKey),
		DefaultLocation: defaultLocation,
	}

	resp.ResourceData = providerData
	resp.DataSourceData = providerData
}

func (p *IDCloudHostProvider) Resources(ctx context.Context) []func() resource.Resource {
	return []func() resource.Resource{
		NewVMResource,
	}
}

func (p *IDCloudHostProvider) DataSources(ctx context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{}
}
