package sqlite

import (
	"context"
	"database/sql"
	"encoding/json"
	"strings"

	"github.com/lunitide/lunitide/internal/mroapp"
	"github.com/oklog/ulid/v2"
)

func (s *Store) ListDueItems(ctx context.Context) ([]mroapp.DueItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT due_id,scope_id,kind,limit_value,used_value,due_at,source,updated_at FROM mro_due_items ORDER BY updated_at, due_id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.DueItem{}
	for rows.Next() {
		var v mroapp.DueItem
		var used sql.NullFloat64
		if err = rows.Scan(&v.ID, &v.ScopeID, &v.Kind, &v.LimitValue, &used, &v.DueAt, &v.Source, &v.UpdatedAt); err != nil {
			return nil, err
		}
		if used.Valid {
			v.UsedValue = used.Float64
		} else {
			v.UsedMissing = true
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) UpsertDueItem(ctx context.Context, row mroapp.DueItem) error {
	var used any
	if !row.UsedMissing {
		used = row.UsedValue
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_due_items(due_id,scope_id,kind,limit_value,used_value,due_at,source,updated_at) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(due_id) DO UPDATE SET scope_id=excluded.scope_id,kind=excluded.kind,limit_value=excluded.limit_value,used_value=excluded.used_value,due_at=excluded.due_at,source=excluded.source,updated_at=excluded.updated_at`,
		row.ID, row.ScopeID, row.Kind, row.LimitValue, used, row.DueAt, row.Source, row.UpdatedAt)
	return err
}

func (s *Store) ListTools(ctx context.Context) ([]mroapp.Tool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tool_id,tool_no,sn,location,holder,calib_due,status,updated_at FROM mro_tools ORDER BY tool_no, tool_id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.Tool{}
	for rows.Next() {
		var v mroapp.Tool
		if err = rows.Scan(&v.ID, &v.ToolNo, &v.SN, &v.Location, &v.Holder, &v.CalibDue, &v.Status, &v.UpdatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) UpsertTool(ctx context.Context, row mroapp.Tool) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_tools(tool_id,tool_no,sn,location,holder,calib_due,status,updated_at) VALUES(?,?,?,?,?,?,?,?)
ON CONFLICT(tool_id) DO UPDATE SET tool_no=excluded.tool_no,sn=excluded.sn,location=excluded.location,holder=excluded.holder,calib_due=excluded.calib_due,status=excluded.status,updated_at=excluded.updated_at`,
		row.ID, row.ToolNo, row.SN, row.Location, row.Holder, row.CalibDue, row.Status, row.UpdatedAt)
	return err
}

func (s *Store) InsertToolLoan(ctx context.Context, row mroapp.ToolLoan) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_tool_loans(loan_id,tool_id,holder,out_at,in_at) VALUES(?,?,?,?,?)`,
		row.ID, row.ToolID, row.Holder, row.OutAt, row.InAt)
	return err
}

func (s *Store) ListChemLots(ctx context.Context) ([]mroapp.ChemLot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT lot_id,lot_no,parent_lot_id,qty,expires,sds_doc FROM mro_chem_lots ORDER BY lot_no, lot_id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.ChemLot{}
	for rows.Next() {
		var v mroapp.ChemLot
		var parent sql.NullString
		if err = rows.Scan(&v.ID, &v.LotNo, &parent, &v.Qty, &v.Expires, &v.SDSDoc); err != nil {
			return nil, err
		}
		if parent.Valid {
			v.ParentLotID = parent.String
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) ListChemUses(ctx context.Context) ([]mroapp.ChemUse, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT use_id,lot_id,tail_no,wo,tech FROM mro_chem_uses ORDER BY use_id LIMIT 400`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.ChemUse{}
	for rows.Next() {
		var v mroapp.ChemUse
		if err = rows.Scan(&v.ID, &v.LotID, &v.TailNo, &v.WO, &v.Tech); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) UpsertChemLot(ctx context.Context, row mroapp.ChemLot) error {
	var parent any
	if strings.TrimSpace(row.ParentLotID) != "" {
		parent = row.ParentLotID
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_chem_lots(lot_id,lot_no,parent_lot_id,qty,expires,sds_doc) VALUES(?,?,?,?,?,?)
ON CONFLICT(lot_id) DO UPDATE SET lot_no=excluded.lot_no,parent_lot_id=excluded.parent_lot_id,qty=excluded.qty,expires=excluded.expires,sds_doc=excluded.sds_doc`,
		row.ID, row.LotNo, parent, row.Qty, row.Expires, row.SDSDoc)
	return err
}

func (s *Store) InsertChemUse(ctx context.Context, row mroapp.ChemUse) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_chem_uses(use_id,lot_id,tail_no,wo,tech) VALUES(?,?,?,?,?)`,
		row.ID, row.LotID, row.TailNo, row.WO, row.Tech)
	return err
}

func (s *Store) ListKits(ctx context.Context) ([]mroapp.Kit, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kit_id,name FROM mro_kits ORDER BY name, kit_id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.Kit{}
	for rows.Next() {
		var v mroapp.Kit
		if err = rows.Scan(&v.ID, &v.Name); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) ListKitItems(ctx context.Context) ([]mroapp.KitItem, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT kit_id,pn,required,on_hand FROM mro_kit_items ORDER BY kit_id, pn LIMIT 800`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.KitItem{}
	for rows.Next() {
		var v mroapp.KitItem
		if err = rows.Scan(&v.KitID, &v.PN, &v.Required, &v.OnHand); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) UpsertKit(ctx context.Context, row mroapp.Kit) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_kits(kit_id,name) VALUES(?,?) ON CONFLICT(kit_id) DO UPDATE SET name=excluded.name`, row.ID, row.Name)
	return err
}

func (s *Store) UpsertKitItem(ctx context.Context, row mroapp.KitItem) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_kit_items(kit_id,pn,required,on_hand) VALUES(?,?,?,?)
ON CONFLICT(kit_id,pn) DO UPDATE SET required=excluded.required,on_hand=excluded.on_hand`,
		row.KitID, row.PN, row.Required, row.OnHand)
	return err
}

func (s *Store) ListPartsStock(ctx context.Context) ([]mroapp.PartsStock, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pn,qty,source FROM mro_parts_stock ORDER BY pn LIMIT 400`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.PartsStock{}
	for rows.Next() {
		var v mroapp.PartsStock
		if err = rows.Scan(&v.PN, &v.Qty, &v.Source); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) ListAlternates(ctx context.Context) ([]mroapp.Alternate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT pn_from,pn_to,cert_ok,effectivity FROM mro_alternates ORDER BY pn_from, pn_to LIMIT 400`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.Alternate{}
	for rows.Next() {
		var v mroapp.Alternate
		var cert int
		if err = rows.Scan(&v.PNFrom, &v.PNTo, &cert, &v.Effectivity); err != nil {
			return nil, err
		}
		v.CertOK = cert == 1
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) UpsertPartsStock(ctx context.Context, row mroapp.PartsStock) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_parts_stock(pn,qty,source) VALUES(?,?,?) ON CONFLICT(pn) DO UPDATE SET qty=excluded.qty,source=excluded.source`,
		row.PN, row.Qty, row.Source)
	return err
}

func (s *Store) UpsertAlternate(ctx context.Context, row mroapp.Alternate) error {
	cert := 0
	if row.CertOK {
		cert = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_alternates(pn_from,pn_to,cert_ok,effectivity) VALUES(?,?,?,?)
ON CONFLICT(pn_from,pn_to) DO UPDATE SET cert_ok=excluded.cert_ok,effectivity=excluded.effectivity`,
		row.PNFrom, row.PNTo, cert, row.Effectivity)
	return err
}

func (s *Store) ListWorkPackages(ctx context.Context) ([]mroapp.WorkPackage, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT package_id,title,sources_json,hours,created_at FROM mro_work_packages ORDER BY created_at, package_id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.WorkPackage{}
	for rows.Next() {
		var v mroapp.WorkPackage
		var raw string
		if err = rows.Scan(&v.ID, &v.Title, &raw, &v.Hours, &v.CreatedAt); err != nil {
			return nil, err
		}
		_ = json.Unmarshal([]byte(raw), &v.Sources)
		if v.Sources == nil {
			v.Sources = []string{}
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) UpsertWorkPackage(ctx context.Context, row mroapp.WorkPackage) error {
	raw, err := json.Marshal(row.Sources)
	if err != nil {
		return err
	}
	if len(raw) < 2 {
		raw = []byte("[]")
	}
	_, err = s.db.ExecContext(ctx, `INSERT INTO mro_work_packages(package_id,title,sources_json,hours,created_at) VALUES(?,?,?,?,?)
ON CONFLICT(package_id) DO UPDATE SET title=excluded.title,sources_json=excluded.sources_json,hours=excluded.hours`,
		row.ID, row.Title, string(raw), row.Hours, row.CreatedAt)
	return err
}

func (s *Store) ListScheduleAssignments(ctx context.Context) ([]mroapp.ScheduleAssignment, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT tail_no,check_name,start_on,end_on,hours,skill FROM mro_schedule_assignments ORDER BY tail_no, assign_id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.ScheduleAssignment{}
	for rows.Next() {
		var v mroapp.ScheduleAssignment
		if err = rows.Scan(&v.TailNo, &v.CheckName, &v.Start, &v.End, &v.Hours, &v.Skill); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) ListCapacitySlots(ctx context.Context) ([]mroapp.CapacitySlot, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT skill,hours FROM mro_capacity_slots ORDER BY skill LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.CapacitySlot{}
	for rows.Next() {
		var v mroapp.CapacitySlot
		if err = rows.Scan(&v.Skill, &v.Hours); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) ListIntervalRules(ctx context.Context) ([]mroapp.IntervalRule, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT rule_id,task_key,interval_value,unit,version,effective_from,source_cite FROM mro_interval_rules ORDER BY task_key LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.IntervalRule{}
	for rows.Next() {
		var v mroapp.IntervalRule
		if err = rows.Scan(&v.ID, &v.TaskKey, &v.IntervalValue, &v.Unit, &v.Version, &v.EffectiveFrom, &v.SourceCite); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) InsertIntervalChangeDraft(ctx context.Context, taskKey, mpdCite, fleetCite, createdAt string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_interval_change_drafts(draft_id,task_key,mpd_cite,fleet_cite,state,created_at) VALUES(?,?,?,?,?,?)`,
		ulid.Make().String(), taskKey, mpdCite, fleetCite, "draft", createdAt)
	return err
}

func (s *Store) ListAOGTails(ctx context.Context) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT DISTINCT tail_no FROM mro_aog_cases WHERE state != 'rejected' ORDER BY tail_no LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var tail string
		if err = rows.Scan(&tail); err != nil {
			return nil, err
		}
		items = append(items, tail)
	}
	return items, rows.Err()
}

func (s *Store) InsertOpsTodos(ctx context.Context, rows []mroapp.OpsTodo) error {
	for _, row := range rows {
		if _, err := s.db.ExecContext(ctx, `INSERT INTO mro_ops_todos(todo_id,kind,ref,status,detail,created_at) VALUES(?,?,?,?,?,?)`,
			row.ID, row.Kind, row.Ref, row.Status, row.Detail, row.CreatedAt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListOpsTodos(ctx context.Context) ([]mroapp.OpsTodo, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT todo_id,kind,ref,status,detail,created_at FROM mro_ops_todos ORDER BY created_at, todo_id LIMIT 200`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.OpsTodo{}
	for rows.Next() {
		var v mroapp.OpsTodo
		if err = rows.Scan(&v.ID, &v.Kind, &v.Ref, &v.Status, &v.Detail, &v.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

// --- P1 write-path store methods -------------------------------------------

func (s *Store) RecordUtilization(ctx context.Context, row mroapp.UtilizationEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_utilization_events(event_id,scope_id,hours,cycles,battery_cycles,created_at) VALUES(?,?,?,?,?,?)`,
		row.ID, row.ScopeID, row.Hours, row.Cycles, row.BatteryCycles, row.CreatedAt)
	return err
}

func (s *Store) ListUtilizationEvents(ctx context.Context) ([]mroapp.UtilizationEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,scope_id,hours,cycles,battery_cycles,created_at FROM mro_utilization_events ORDER BY created_at, event_id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.UtilizationEvent{}
	for rows.Next() {
		var v mroapp.UtilizationEvent
		if err = rows.Scan(&v.ID, &v.ScopeID, &v.Hours, &v.Cycles, &v.BatteryCycles, &v.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) CloseOpenToolLoan(ctx context.Context, toolID, inAt string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE mro_tool_loans SET in_at=? WHERE tool_id=? AND in_at=''`, inAt, toolID)
	return err
}

func (s *Store) InsertScheduleAssignment(ctx context.Context, id string, row mroapp.ScheduleAssignment) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_schedule_assignments(assign_id,tail_no,check_name,start_on,end_on,hours,skill) VALUES(?,?,?,?,?,?,?)`,
		id, row.TailNo, row.CheckName, row.Start, row.End, row.Hours, row.Skill)
	return err
}

func (s *Store) UpsertCapacitySlot(ctx context.Context, skill string, hours float64) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mro_capacity_slots WHERE skill=?`, skill); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_capacity_slots(slot_id,skill,hours) VALUES(?,?,?)`, ulid.Make().String(), skill, hours)
	return err
}

func (s *Store) UpsertIntervalRule(ctx context.Context, row mroapp.IntervalRule) error {
	if _, err := s.db.ExecContext(ctx, `DELETE FROM mro_interval_rules WHERE task_key=?`, row.TaskKey); err != nil {
		return err
	}
	version := row.Version
	if strings.TrimSpace(version) == "" {
		version = "1"
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_interval_rules(rule_id,task_key,interval_value,unit,version,effective_from,source_cite) VALUES(?,?,?,?,?,?,?)`,
		row.ID, row.TaskKey, row.IntervalValue, row.Unit, version, row.EffectiveFrom, row.SourceCite)
	return err
}

func (s *Store) InsertWorkPackageTasks(ctx context.Context, packageID string, taskKeys []string) error {
	for _, key := range taskKeys {
		if strings.TrimSpace(key) == "" {
			continue
		}
		if _, err := s.db.ExecContext(ctx, `INSERT OR IGNORE INTO mro_wp_tasks(package_id,task_key) VALUES(?,?)`, packageID, key); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) ListWorkPackageTasks(ctx context.Context, packageID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT task_key FROM mro_wp_tasks WHERE package_id=? ORDER BY task_key LIMIT 400`, packageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []string{}
	for rows.Next() {
		var key string
		if err = rows.Scan(&key); err != nil {
			return nil, err
		}
		items = append(items, key)
	}
	return items, rows.Err()
}

// --- P2 low-altitude / AOG / PO store methods ------------------------------

func (s *Store) UpsertComponent(ctx context.Context, row mroapp.Component) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_components(component_id,sn,pn,life_count,created_at) VALUES(?,?,?,?,?)
ON CONFLICT(component_id) DO UPDATE SET sn=excluded.sn,pn=excluded.pn,life_count=excluded.life_count`,
		row.ID, row.SN, row.PN, row.LifeCount, row.CreatedAt)
	return err
}

func (s *Store) ListComponents(ctx context.Context) ([]mroapp.Component, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT component_id,sn,pn,life_count,created_at FROM mro_components ORDER BY created_at, component_id LIMIT 400`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.Component{}
	for rows.Next() {
		var v mroapp.Component
		if err = rows.Scan(&v.ID, &v.SN, &v.PN, &v.LifeCount, &v.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) InsertLifeEvent(ctx context.Context, row mroapp.LifeEvent) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_life_events(event_id,component_id,kind,occurred_at,note) VALUES(?,?,?,?,?)`,
		row.ID, row.ComponentID, row.Kind, row.OccurredAt, row.Note)
	return err
}

func (s *Store) ListLifeEvents(ctx context.Context) ([]mroapp.LifeEvent, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT event_id,component_id,kind,occurred_at,note FROM mro_life_events ORDER BY occurred_at, event_id LIMIT 1000`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.LifeEvent{}
	for rows.Next() {
		var v mroapp.LifeEvent
		if err = rows.Scan(&v.ID, &v.ComponentID, &v.Kind, &v.OccurredAt, &v.Note); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) InsertPirepDraft(ctx context.Context, row mroapp.PirepDraft) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_pirep_drafts(draft_id,tail_no,body_json,state,created_at) VALUES(?,?,?,?,?)`,
		row.ID, row.TailNo, row.BodyJSON, row.State, row.CreatedAt)
	return err
}

func (s *Store) ListPirepDrafts(ctx context.Context) ([]mroapp.PirepDraft, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT draft_id,tail_no,body_json,state,created_at FROM mro_pirep_drafts ORDER BY created_at, draft_id LIMIT 400`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.PirepDraft{}
	for rows.Next() {
		var v mroapp.PirepDraft
		if err = rows.Scan(&v.ID, &v.TailNo, &v.BodyJSON, &v.State, &v.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) InsertAOGCase(ctx context.Context, row mroapp.AOGCase) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_aog_cases(case_id,tail_no,pn,qty,note,state,created_at) VALUES(?,?,?,?,?,?,?)`,
		row.ID, row.TailNo, row.PN, row.Qty, row.Note, row.State, row.CreatedAt)
	return err
}

func (s *Store) ListAOGCases(ctx context.Context) ([]mroapp.AOGCase, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT case_id,tail_no,pn,qty,note,state,created_at FROM mro_aog_cases ORDER BY created_at, case_id LIMIT 400`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.AOGCase{}
	for rows.Next() {
		var v mroapp.AOGCase
		if err = rows.Scan(&v.ID, &v.TailNo, &v.PN, &v.Qty, &v.Note, &v.State, &v.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) InsertPODraft(ctx context.Context, row mroapp.PODraft) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO mro_po_drafts(draft_id,pn,qty,price,state,created_at) VALUES(?,?,?,?,?,?)`,
		row.ID, row.PN, row.Qty, row.Price, row.State, row.CreatedAt)
	return err
}

func (s *Store) ListPODrafts(ctx context.Context) ([]mroapp.PODraft, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT draft_id,pn,qty,price,state,created_at FROM mro_po_drafts ORDER BY created_at, draft_id LIMIT 400`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []mroapp.PODraft{}
	for rows.Next() {
		var v mroapp.PODraft
		if err = rows.Scan(&v.ID, &v.PN, &v.Qty, &v.Price, &v.State, &v.CreatedAt); err != nil {
			return nil, err
		}
		items = append(items, v)
	}
	return items, rows.Err()
}

func (s *Store) UpdatePirepState(ctx context.Context, id, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE mro_pirep_drafts SET state=? WHERE draft_id=? AND state='draft'`, state, id)
	return draftStateResult(res, err)
}

func (s *Store) UpdateAOGState(ctx context.Context, id, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE mro_aog_cases SET state=? WHERE case_id=? AND state='draft'`, state, id)
	return draftStateResult(res, err)
}

func (s *Store) UpdatePOState(ctx context.Context, id, state string) error {
	res, err := s.db.ExecContext(ctx, `UPDATE mro_po_drafts SET state=? WHERE draft_id=? AND state='draft'`, state, id)
	return draftStateResult(res, err)
}

func draftStateResult(res interface{ RowsAffected() (int64, error) }, err error) error {
	if err != nil {
		return err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return err
	}
	if n == 0 {
		return mroapp.ErrNotFound
	}
	return nil
}
