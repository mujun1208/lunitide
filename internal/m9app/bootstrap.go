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
