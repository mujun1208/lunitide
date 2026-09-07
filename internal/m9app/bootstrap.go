package m9app

import "context"

const defaultOrgName = "Lunitide默认组织"

// EnsureDefaultOrgBinding auto-provisions the single-user desktop org context:
// create a default org when none exists, bind the local operator, and activate
// it so backend automation (budget, audit, workspace grants) always has a
// verified org scope without a front-end admin surface.
func EnsureDefaultOrgBinding(ctx context.Context, admin *OrgAdminService) error {
	if admin == nil {
		return nil
	}
	sum, err := admin.Summary(ctx)
	if err != nil {
		return err
	}
	if sum.BoundOrgID != "" {
		return nil
	}
	if selection, ok := admin.binding.(interface {
		Selection(context.Context) (bool, bool, error)
	}); ok {
		exists, _, err := selection.Selection(ctx)
		if err != nil {
			return err
		}
		if exists {
			return nil
		} // An explicitly saved personal scope survives restart.
	}
	orgID := ""
	if len(sum.Orgs) > 0 {
		orgID = sum.Orgs[0].OrgID
	} else {
		view, err := admin.CreateOrg(ctx, defaultOrgName)
		if err != nil {
			return err
		}
		orgID = view.OrgID
	}
	if _, err := admin.Switch(ctx, orgID); err != nil {
		return err
	}
	_, err = admin.Activate(ctx)
	return err
}

// RestoreLegacyPersonalBinding restores the original local desktop data view
// only when an old implicit default binding is unambiguous. Existing selected
// scopes, custom/multiple organizations and any organization-owned data are
// preserved. No records are moved or reassigned to another owner.
func RestoreLegacyPersonalBinding(ctx context.Context, admin *OrgAdminService, personal, organization bool) error {
	if admin == nil || !personal || organization {
		return nil
	}
	selection, ok := admin.binding.(interface {
		Selection(context.Context) (bool, bool, error)
	})
	if !ok {
		return nil
	}
	_, chosen, err := selection.Selection(ctx)
	if err != nil || chosen {
		return err
	}
	summary, err := admin.Summary(ctx)
	if err != nil {
		return err
	}
	if len(summary.Orgs) == 0 && summary.BoundOrgID == "" {
		return admin.SelectPersonal(ctx)
	}
	if len(summary.Orgs) == 1 && summary.Orgs[0].Name == defaultOrgName && summary.Orgs[0].State == "active" && (summary.BoundOrgID == "" || summary.BoundOrgID == summary.Orgs[0].OrgID) {
		return admin.SelectPersonal(ctx)
	}
	return nil
}
