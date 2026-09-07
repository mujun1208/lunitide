package sqlite

import "context"

// DesktopScopePresence reads only ownership markers, never user content. All
// three formerly unscoped desktop stores participate in the upgrade decision.
func (s *Store) DesktopScopePresence(ctx context.Context) (personal, organization bool, err error) {
	for _, table := range []string{
		"projects", "asset_templates", "mro_aircraft", "mro_alternates", "mro_aog_cases",
		"mro_capacity_slots", "mro_chem_lots", "mro_chem_uses", "mro_components", "mro_defect_drafts",
		"mro_due_items", "mro_interval_change_drafts", "mro_interval_rules", "mro_kit_items", "mro_kits",
		"mro_life_events", "mro_manual_docs", "mro_manuals", "mro_ops_todos", "mro_parts_stock",
		"mro_pirep_drafts", "mro_po_drafts", "mro_schedule_assignments", "mro_task_card_templates",
		"mro_tool_loans", "mro_tools", "mro_utilization_events", "mro_work_packages",
	} {
		var p, o bool
		// Table identifiers come exclusively from the static schema list above.
		if err = s.db.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM `+table+` WHERE org_id IS NULL), EXISTS(SELECT 1 FROM `+table+` WHERE org_id IS NOT NULL)`).Scan(&p, &o); err != nil {
			return false, false, err
		}
		personal = personal || p
		organization = organization || o
		if personal && organization {
			return personal, organization, nil
		}
	}
	return personal, organization, nil
}
