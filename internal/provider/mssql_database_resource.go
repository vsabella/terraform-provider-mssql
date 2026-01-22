package provider

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/stringplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"
	"github.com/vsabella/terraform-provider-mssql/internal/core"
	"github.com/vsabella/terraform-provider-mssql/internal/mssql"
)

// Ensure provider defined types fully satisfy framework interfaces.
var _ resource.Resource = &MssqlDatabaseResource{}
var _ resource.ResourceWithImportState = &MssqlDatabaseResource{}
var resLock sync.Mutex

func NewMssqlDatabaseResource() resource.Resource {
	return &MssqlDatabaseResource{}
}

type MssqlDatabaseResource struct {
	ctx core.ProviderData
}

type MssqlDatabaseResourceModel struct {
	Id                          types.String `tfsdk:"id"`
	Name                        types.String `tfsdk:"name"`
	Collation                   types.String `tfsdk:"collation"`
	CompatibilityLevel          types.Int64  `tfsdk:"compatibility_level"`
	RecoveryModel               types.String `tfsdk:"recovery_model"`
	ReadCommittedSnapshot       types.Bool   `tfsdk:"read_committed_snapshot"`
	AllowSnapshotIsolation      types.Bool   `tfsdk:"allow_snapshot_isolation"`
	AcceleratedDatabaseRecovery types.Bool   `tfsdk:"accelerated_database_recovery"`
	AutoClose                   types.Bool   `tfsdk:"auto_close"`
	AutoShrink                  types.Bool   `tfsdk:"auto_shrink"`
	AutoCreateStats             types.Bool   `tfsdk:"auto_create_stats"`
	AutoUpdateStats             types.Bool   `tfsdk:"auto_update_stats"`
	AutoUpdateStatsAsync        types.Bool   `tfsdk:"auto_update_stats_async"`
	ScopedConfigurations        types.Set    `tfsdk:"scoped_configuration"`
}

type ScopedConfigurationModel struct {
	Name              types.String `tfsdk:"name"`
	Value             types.String `tfsdk:"value"`
	ValueForSecondary types.String `tfsdk:"value_for_secondary"`
}

func (r *MssqlDatabaseResource) Metadata(ctx context.Context, req resource.MetadataRequest, resp *resource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_database"
}

func (r *MssqlDatabaseResource) Schema(ctx context.Context, req resource.SchemaRequest, resp *resource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Manages a SQL Server database including engine options and scoped configurations. **Note:** Destroy removes the resource from Terraform state but does not drop the database from the server.",

		Attributes: map[string]schema.Attribute{
			"id": schema.StringAttribute{
				MarkdownDescription: "Resource identifier in format `<server_id>/<database>` where `server_id` is `host:port`.",
				Computed:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.UseStateForUnknown(),
				},
			},
			"name": schema.StringAttribute{
				MarkdownDescription: "Database name. Changing this forces a new resource to be created.",
				Required:            true,
				PlanModifiers: []planmodifier.String{
					stringplanmodifier.RequiresReplace(),
				},
			},
			"collation": schema.StringAttribute{
				MarkdownDescription: "Database collation. If not specified, uses the server default collation. Changing this updates the database default for new objects only; existing columns keep their current collations and a change may require downtime.",
				Optional:            true,
				Computed:            true,
			},
			"compatibility_level": schema.Int64Attribute{
				MarkdownDescription: "Database compatibility level (e.g., 150 for SQL Server 2019, 160 for SQL Server 2022). If not specified, the existing setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"recovery_model": schema.StringAttribute{
				MarkdownDescription: "Recovery model: FULL, BULK_LOGGED, or SIMPLE. If not specified, the existing setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"read_committed_snapshot": schema.BoolAttribute{
				MarkdownDescription: "Enable READ_COMMITTED_SNAPSHOT isolation. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"allow_snapshot_isolation": schema.BoolAttribute{
				MarkdownDescription: "Allow SNAPSHOT isolation level. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"accelerated_database_recovery": schema.BoolAttribute{
				MarkdownDescription: "Enable Accelerated Database Recovery (ADR). Available in SQL Server 2019+ and Azure SQL. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"auto_close": schema.BoolAttribute{
				MarkdownDescription: "Automatically close the database when the last user exits. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"auto_shrink": schema.BoolAttribute{
				MarkdownDescription: "Automatically shrink the database files. Not recommended for production. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"auto_create_stats": schema.BoolAttribute{
				MarkdownDescription: "Automatically create statistics on columns. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"auto_update_stats": schema.BoolAttribute{
				MarkdownDescription: "Automatically update statistics. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
			"auto_update_stats_async": schema.BoolAttribute{
				MarkdownDescription: "Update statistics asynchronously. If not specified, the existing database setting is preserved.",
				Optional:            true,
				Computed:            true,
			},
		},
		Blocks: map[string]schema.Block{
			"scoped_configuration": schema.SetNestedBlock{
				MarkdownDescription: "Database scoped configuration settings (ALTER DATABASE SCOPED CONFIGURATION).",
				NestedObject: schema.NestedBlockObject{
					Attributes: map[string]schema.Attribute{
						"name": schema.StringAttribute{
							MarkdownDescription: "Configuration name (e.g., MAXDOP, LEGACY_CARDINALITY_ESTIMATION).",
							Required:            true,
						},
						"value": schema.StringAttribute{
							MarkdownDescription: "Configuration value.",
							Required:            true,
						},
						"value_for_secondary": schema.StringAttribute{
							MarkdownDescription: "Configuration value for secondary replicas (optional).",
							Optional:            true,
						},
					},
				},
			},
		},
	}
}

func (r *MssqlDatabaseResource) Configure(ctx context.Context, req resource.ConfigureRequest, resp *resource.ConfigureResponse) {
	// Prevent panic if the provider has not been configured.
	if req.ProviderData == nil {
		return
	}
	client, ok := req.ProviderData.(*core.ProviderData)

	if !ok {
		resp.Diagnostics.AddError(
			"Unexpected Resource Configure Type",
			fmt.Sprintf("Expected *core.ProviderData, got: %T. Please report this issue to the provider developers.", req.ProviderData),
		)

		return
	}

	r.ctx = *client
}

func (r *MssqlDatabaseResource) Create(ctx context.Context, req resource.CreateRequest, resp *resource.CreateResponse) {
	resLock.Lock()
	defer resLock.Unlock()

	var data MssqlDatabaseResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &data)...)

	if resp.Diagnostics.HasError() {
		return
	}

	_, err := r.ctx.Client.CreateDatabase(ctx, data.Name.ValueString())
	if err != nil {
		resp.Diagnostics.AddError(fmt.Sprintf("Error creating database %s", data.Name.ValueString()), err.Error())
		return
	}

	data.Id = types.StringValue(fmt.Sprintf("%s/%s", r.ctx.ServerID, data.Name.ValueString()))
	tflog.Debug(ctx, fmt.Sprintf("Created database %s", data.Name.ValueString()))

	// Apply database options if any are set
	if err := r.applyDatabaseOptions(ctx, &data); err != nil {
		resp.Diagnostics.AddError("Error applying database options", err.Error())
		return
	}

	// Apply scoped configurations (fail-fast to avoid silent drift). No prior managed set on create.
	if err := r.applyScopedConfigurations(ctx, &data, nil); err != nil {
		resp.Diagnostics.AddError("Error applying scoped configurations", err.Error())
		return
	}

	// Read back current state for computed values
	if err := r.refreshDatabaseState(ctx, &data); err != nil {
		resp.Diagnostics.AddWarning("Error refreshing database state", err.Error())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &data)...)
}

func (r *MssqlDatabaseResource) Read(ctx context.Context, req resource.ReadRequest, resp *resource.ReadResponse) {
	var state MssqlDatabaseResourceModel

	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	databaseName := state.Name.ValueString()
	if state.Name.IsUnknown() || state.Name.IsNull() || databaseName == "" {
		dbName, err := parseDatabaseId(state.Id.ValueString())
		if err != nil {
			resp.Diagnostics.AddError("Invalid database ID", err.Error())
			return
		}
		databaseName = dbName
	}
	if databaseName == "" {
		resp.Diagnostics.AddError("Unable to read database", "Database name is missing from state")
		return
	}

	db, err := r.ctx.Client.GetDatabase(ctx, databaseName)

	if errors.Is(err, sql.ErrNoRows) {
		resp.Diagnostics.AddWarning("Database not found, removing from state", fmt.Sprintf("Database %s not found, removing from state", databaseName))
		resp.State.RemoveResource(ctx)
		return
	} else if err != nil {
		resp.Diagnostics.AddError("Unable to read database", fmt.Sprintf("Unable to read database %s. Error: %s", databaseName, err))
		return
	}

	state.Id = types.StringValue(fmt.Sprintf("%s/%s", r.ctx.ServerID, db.Name))
	state.Name = types.StringValue(db.Name)

	if err := r.refreshDatabaseState(ctx, &state); err != nil {
		resp.Diagnostics.AddWarning("Error refreshing database state", err.Error())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &state)...)
}

func (r *MssqlDatabaseResource) Update(ctx context.Context, req resource.UpdateRequest, resp *resource.UpdateResponse) {
	resLock.Lock()
	defer resLock.Unlock()

	var plan, state MssqlDatabaseResourceModel

	resp.Diagnostics.Append(req.Plan.Get(ctx, &plan)...)
	resp.Diagnostics.Append(req.State.Get(ctx, &state)...)
	if resp.Diagnostics.HasError() {
		return
	}

	// we don't support updating database name as there should not be any reason to do so.
	if plan.Name.ValueString() != state.Name.ValueString() {
		resp.Diagnostics.AddError("Unable to update database", fmt.Sprintf("Updating database name is not supported. Database name cannot be changed from %s to %s.", state.Name.ValueString(), plan.Name.ValueString()))
		return
	}

	if !plan.Collation.IsNull() && !plan.Collation.IsUnknown() && !state.Collation.IsNull() && !state.Collation.IsUnknown() {
		desired := strings.TrimSpace(plan.Collation.ValueString())
		current := strings.TrimSpace(state.Collation.ValueString())
		if desired != "" && !strings.EqualFold(desired, current) {
			resp.Diagnostics.AddWarning(
				"Collation change affects only new objects",
				"Updating database collation changes the default for new objects only; existing columns retain their current collations. This operation can be disruptive and may require downtime.",
			)
		}
	}

	var priorConfigs []ScopedConfigurationModel
	if !state.ScopedConfigurations.IsNull() && !state.ScopedConfigurations.IsUnknown() {
		if diags := state.ScopedConfigurations.ElementsAs(ctx, &priorConfigs, false); diags.HasError() {
			resp.Diagnostics.Append(diags...)
			return
		}
	}

	// Apply database options
	if err := r.applyDatabaseOptions(ctx, &plan); err != nil {
		resp.Diagnostics.AddError("Error applying database options", err.Error())
		return
	}

	// Apply scoped configurations (fail-fast to avoid silent drift)
	if err := r.applyScopedConfigurations(ctx, &plan, priorConfigs); err != nil {
		resp.Diagnostics.AddError("Error applying scoped configurations", err.Error())
		return
	}

	// Refresh state for computed values
	if err := r.refreshDatabaseState(ctx, &plan); err != nil {
		resp.Diagnostics.AddWarning("Error refreshing database state", err.Error())
	}

	resp.Diagnostics.Append(resp.State.Set(ctx, &plan)...)
}

func (r *MssqlDatabaseResource) Delete(ctx context.Context, req resource.DeleteRequest, resp *resource.DeleteResponse) {
	resLock.Lock()
	defer resLock.Unlock()

	var data MssqlDatabaseResourceModel

	// Read Terraform prior state data into the model
	resp.Diagnostics.Append(req.State.Get(ctx, &data)...)
	if resp.Diagnostics.HasError() {
		return
	}

	tflog.Warn(ctx, fmt.Sprintf("Database %s will not be deleted. Terraform will remove it from state but the database will remain on the server.", data.Name.ValueString()))
	resp.Diagnostics.AddWarning(
		"Database not deleted",
		fmt.Sprintf("Database %s was not dropped. The resource is removed from state but the database remains on the server.", data.Name.ValueString()),
	)
}

func (r *MssqlDatabaseResource) ImportState(ctx context.Context, req resource.ImportStateRequest, resp *resource.ImportStateResponse) {
	// Import ID must be <server_id>/<database>
	dbName, err := parseDatabaseId(req.ID)
	if err != nil {
		resp.Diagnostics.AddError("Invalid import ID", err.Error())
		return
	}

	db, err := r.ctx.Client.GetDatabase(ctx, dbName)
	if err != nil {
		resp.Diagnostics.AddError("Unable to import database", fmt.Sprintf("Database %s not found or error occurred: %s", dbName, err))
		return
	}

	// Do NOT import all existing scoped configurations into state.
	// We only want to manage the scoped_configuration blocks explicitly declared in config.
	// Importing the full server view here can cause Terraform to plan clearing unrelated settings.

	// Set basic attributes
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("id"), fmt.Sprintf("%s/%s", r.ctx.ServerID, db.Name))...)
	resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("name"), db.Name)...)

	// Get database options
	opts, err := r.ctx.Client.GetDatabaseOptions(ctx, db.Name)
	if err == nil {
		resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("collation"), opts.Collation)...)
		if opts.CompatibilityLevel != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("compatibility_level"), int64(*opts.CompatibilityLevel))...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("compatibility_level"), types.Int64Null())...)
		}
		if opts.RecoveryModel != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("recovery_model"), *opts.RecoveryModel)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("recovery_model"), types.StringNull())...)
		}
		if opts.ReadCommittedSnapshot != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("read_committed_snapshot"), *opts.ReadCommittedSnapshot)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("read_committed_snapshot"), types.BoolNull())...)
		}
		if opts.AllowSnapshotIsolation != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_snapshot_isolation"), *opts.AllowSnapshotIsolation)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("allow_snapshot_isolation"), types.BoolNull())...)
		}
		if opts.AcceleratedDatabaseRecovery != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("accelerated_database_recovery"), *opts.AcceleratedDatabaseRecovery)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("accelerated_database_recovery"), types.BoolNull())...)
		}
		if opts.AutoClose != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_close"), *opts.AutoClose)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_close"), types.BoolNull())...)
		}
		if opts.AutoShrink != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_shrink"), *opts.AutoShrink)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_shrink"), types.BoolNull())...)
		}
		if opts.AutoCreateStats != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_create_stats"), *opts.AutoCreateStats)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_create_stats"), types.BoolNull())...)
		}
		if opts.AutoUpdateStats != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_update_stats"), *opts.AutoUpdateStats)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_update_stats"), types.BoolNull())...)
		}
		if opts.AutoUpdateStatsAsync != nil {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_update_stats_async"), *opts.AutoUpdateStatsAsync)...)
		} else {
			resp.Diagnostics.Append(resp.State.SetAttribute(ctx, path.Root("auto_update_stats_async"), types.BoolNull())...)
		}
	}
}

func parseDatabaseId(id string) (string, error) {
	parts := strings.Split(id, "/")
	if len(parts) != 2 || parts[0] == "" || parts[1] == "" {
		return "", fmt.Errorf("expected id in format <server_id>/<database>, got %q", id)
	}
	db, err := url.QueryUnescape(parts[1])
	if err != nil {
		return "", err
	}
	return db, nil
}

func (r *MssqlDatabaseResource) applyDatabaseOptions(ctx context.Context, data *MssqlDatabaseResourceModel) error {
	// Fetch current options to avoid reapplying disruptive settings (e.g., RCSI WITH ROLLBACK IMMEDIATE).
	current, err := r.ctx.Client.GetDatabaseOptions(ctx, data.Name.ValueString())
	if err != nil {
		return err
	}

	opts := mssql.DatabaseOptions{}
	hasChanges := false

	// Collation (string)
	if !data.Collation.IsNull() && !data.Collation.IsUnknown() {
		desired := data.Collation.ValueString()
		if desired != "" && desired != current.Collation {
			opts.Collation = desired
			hasChanges = true
		}
	}

	// Compatibility level
	if !data.CompatibilityLevel.IsNull() && !data.CompatibilityLevel.IsUnknown() {
		desired := int(data.CompatibilityLevel.ValueInt64())
		if current.CompatibilityLevel == nil || *current.CompatibilityLevel != desired {
			opts.CompatibilityLevel = &desired
			hasChanges = true
		}
	}

	// Recovery model
	if !data.RecoveryModel.IsNull() && !data.RecoveryModel.IsUnknown() {
		desired := data.RecoveryModel.ValueString()
		if current.RecoveryModel == nil || *current.RecoveryModel != desired {
			opts.RecoveryModel = &desired
			hasChanges = true
		}
	}

	compareBool := func(val types.Bool, cur *bool, setter func(v bool)) {
		if val.IsNull() || val.IsUnknown() {
			return
		}
		desired := val.ValueBool()
		if cur == nil || *cur != desired {
			setter(desired)
			hasChanges = true
		}
	}

	compareBool(data.ReadCommittedSnapshot, current.ReadCommittedSnapshot, func(v bool) { opts.ReadCommittedSnapshot = &v })
	compareBool(data.AllowSnapshotIsolation, current.AllowSnapshotIsolation, func(v bool) { opts.AllowSnapshotIsolation = &v })
	compareBool(data.AcceleratedDatabaseRecovery, current.AcceleratedDatabaseRecovery, func(v bool) { opts.AcceleratedDatabaseRecovery = &v })
	compareBool(data.AutoClose, current.AutoClose, func(v bool) { opts.AutoClose = &v })
	compareBool(data.AutoShrink, current.AutoShrink, func(v bool) { opts.AutoShrink = &v })
	compareBool(data.AutoCreateStats, current.AutoCreateStats, func(v bool) { opts.AutoCreateStats = &v })
	compareBool(data.AutoUpdateStats, current.AutoUpdateStats, func(v bool) { opts.AutoUpdateStats = &v })
	compareBool(data.AutoUpdateStatsAsync, current.AutoUpdateStatsAsync, func(v bool) { opts.AutoUpdateStatsAsync = &v })

	if !hasChanges {
		tflog.Debug(ctx, "applyDatabaseOptions: no changes detected, skipping ALTER DATABASE")
		return nil
	}

	return r.ctx.Client.SetDatabaseOptions(ctx, data.Name.ValueString(), opts)
}

// applyScopedConfigurations applies desired configs and clears only those previously managed but now absent.
func (r *MssqlDatabaseResource) applyScopedConfigurations(ctx context.Context, data *MssqlDatabaseResourceModel, previouslyManaged []ScopedConfigurationModel) error {
	// Treat null/unknown as "do not manage"
	if data.ScopedConfigurations.IsNull() || data.ScopedConfigurations.IsUnknown() {
		return nil
	}

	var configs []ScopedConfigurationModel
	diags := data.ScopedConfigurations.ElementsAs(ctx, &configs, false)
	if diags.HasError() {
		return fmt.Errorf("error reading scoped configurations")
	}

	desired := make(map[string]mssql.DatabaseScopedConfiguration)
	for _, cfg := range configs {
		desired[cfg.Name.ValueString()] = mssql.DatabaseScopedConfiguration{
			Name:              cfg.Name.ValueString(),
			Value:             cfg.Value.ValueString(),
			ValueForSecondary: cfg.ValueForSecondary.ValueString(),
		}
	}

	managedNames := make(map[string]struct{})
	for _, cfg := range previouslyManaged {
		managedNames[cfg.Name.ValueString()] = struct{}{}
	}

	// Clear only configurations we previously managed and that are now removed.
	if len(managedNames) > 0 {
		existing, err := r.ctx.Client.GetDatabaseScopedConfigurations(ctx, data.Name.ValueString())
		if err != nil {
			return err
		}
		for _, cur := range existing {
			if _, wasManaged := managedNames[cur.Name]; wasManaged {
				if _, stillDesired := desired[cur.Name]; !stillDesired {
					if err := r.ctx.Client.ClearDatabaseScopedConfiguration(ctx, data.Name.ValueString(), cur.Name); err != nil {
						return fmt.Errorf("failed to clear scoped configuration %s: %w", cur.Name, err)
					}
				}
			}
		}
	}

	for _, cfg := range desired {
		if err := r.ctx.Client.SetDatabaseScopedConfiguration(ctx, data.Name.ValueString(), cfg); err != nil {
			return fmt.Errorf("failed to set scoped configuration %s: %w", cfg.Name, err)
		}
	}

	return nil
}

func (r *MssqlDatabaseResource) refreshDatabaseState(ctx context.Context, data *MssqlDatabaseResourceModel) error {
	opts, err := r.ctx.Client.GetDatabaseOptions(ctx, data.Name.ValueString())
	if err != nil {
		return err
	}

	data.Collation = types.StringValue(opts.Collation)
	if opts.CompatibilityLevel != nil {
		data.CompatibilityLevel = types.Int64Value(int64(*opts.CompatibilityLevel))
	} else {
		data.CompatibilityLevel = types.Int64Null()
	}
	if opts.RecoveryModel != nil {
		data.RecoveryModel = types.StringValue(*opts.RecoveryModel)
	} else {
		data.RecoveryModel = types.StringNull()
	}

	if opts.ReadCommittedSnapshot != nil {
		data.ReadCommittedSnapshot = types.BoolValue(*opts.ReadCommittedSnapshot)
	}
	if opts.AllowSnapshotIsolation != nil {
		data.AllowSnapshotIsolation = types.BoolValue(*opts.AllowSnapshotIsolation)
	}
	if opts.AcceleratedDatabaseRecovery != nil {
		data.AcceleratedDatabaseRecovery = types.BoolValue(*opts.AcceleratedDatabaseRecovery)
	}
	if opts.AutoClose != nil {
		data.AutoClose = types.BoolValue(*opts.AutoClose)
	}
	if opts.AutoShrink != nil {
		data.AutoShrink = types.BoolValue(*opts.AutoShrink)
	}
	if opts.AutoCreateStats != nil {
		data.AutoCreateStats = types.BoolValue(*opts.AutoCreateStats)
	}
	if opts.AutoUpdateStats != nil {
		data.AutoUpdateStats = types.BoolValue(*opts.AutoUpdateStats)
	}
	if opts.AutoUpdateStatsAsync != nil {
		data.AutoUpdateStatsAsync = types.BoolValue(*opts.AutoUpdateStatsAsync)
	}

	// Note: We don't refresh scoped_configurations from the server to avoid overwriting
	// user intent with all server settings. Treat user-specified configurations as source of truth.
	if data.ScopedConfigurations.IsNull() || data.ScopedConfigurations.IsUnknown() {
		data.ScopedConfigurations = types.SetNull(types.ObjectType{
			AttrTypes: map[string]attr.Type{
				"name":                types.StringType,
				"value":               types.StringType,
				"value_for_secondary": types.StringType,
			},
		})
	}

	return nil
}
