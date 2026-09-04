package mroapp

import (
	"context"
	"strings"

	"github.com/oklog/ulid/v2"
)

func (s *Service) ops() (OpsStore, error) {
	if s == nil || s.store == nil {
		return nil, ErrServiceUnavailable
	}
	ops, ok := s.store.(OpsStore)
	if !ok {
		return nil, ErrServiceUnavailable
	}
	return ops, nil
}

func (s *Service) ListDue(ctx context.Context) ([]DueView, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	items, err := ops.ListDueItems(ctx)
	if err != nil {
		return nil, err
	}
	today := s.clock.Now()
	out := make([]DueView, 0, len(items))
	for _, item := range items {
		out = append(out, DueView{DueItem: item, Status: EvaluateDue(item, today)})
	}
	return out, nil
}

func (s *Service) UpsertDueItem(ctx context.Context, row DueItem) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.ID) == "" {
		row.ID = ulid.Make().String()
	}
	if len(row.ID) != 26 || strings.TrimSpace(row.ScopeID) == "" || strings.TrimSpace(row.Kind) == "" {
		return ErrPayloadInvalid
	}
	if strings.TrimSpace(row.UpdatedAt) == "" {
		row.UpdatedAt = s.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	return ops.UpsertDueItem(ctx, row)
}

func (s *Service) ListToolViews(ctx context.Context) ([]ToolView, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	items, err := ops.ListTools(ctx)
	if err != nil {
		return nil, err
	}
	today := s.clock.Now()
	out := make([]ToolView, 0, len(items))
	for _, item := range items {
		view := ToolView{Tool: item}
		if ok, reason := CanCheckoutTool(item, today); !ok {
			view.CheckoutBlocked = reason
		}
		out = append(out, view)
	}
	return out, nil
}

func (s *Service) UpsertTool(ctx context.Context, row Tool) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.ID) == "" {
		row.ID = ulid.Make().String()
	}
	if len(row.ID) != 26 || strings.TrimSpace(row.ToolNo) == "" {
		return ErrPayloadInvalid
	}
	if strings.TrimSpace(row.UpdatedAt) == "" {
		row.UpdatedAt = s.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	}
	if strings.TrimSpace(row.Status) == "" {
		row.Status = "ready"
	}
	return ops.UpsertTool(ctx, row)
}

func (s *Service) CheckoutTool(ctx context.Context, toolID, holder string) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	toolID = strings.TrimSpace(toolID)
	holder = strings.TrimSpace(holder)
	if len(toolID) != 26 || holder == "" {
		return ErrPayloadInvalid
	}
	items, err := ops.ListTools(ctx)
	if err != nil {
		return err
	}
	var tool Tool
	found := false
	for _, item := range items {
		if item.ID == toolID {
			tool = item
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}
	if ok, reason := CanCheckoutTool(tool, s.clock.Now()); !ok {
		return &CheckoutBlockedError{Reason: reason}
	}
	now := s.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	if err := ops.InsertToolLoan(ctx, ToolLoan{
		ID: ulid.Make().String(), ToolID: toolID, Holder: holder, OutAt: now,
	}); err != nil {
		return err
	}
	tool.Holder = holder
	tool.Status = "out"
	tool.UpdatedAt = now
	return ops.UpsertTool(ctx, tool)
}

func (s *Service) ListLotViews(ctx context.Context) ([]LotView, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	lots, err := ops.ListChemLots(ctx)
	if err != nil {
		return nil, err
	}
	uses, err := ops.ListChemUses(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]LotView, 0, len(lots))
	for _, lot := range lots {
		trace := TraceLot(lots, uses, lot.ID)
		out = append(out, LotView{ChemLot: lot, Children: trace.Children, Tails: trace.Tails})
	}
	return out, nil
}

func (s *Service) TraceLotID(ctx context.Context, lotID string) (LotView, error) {
	views, err := s.ListLotViews(ctx)
	if err != nil {
		return LotView{}, err
	}
	lotID = strings.TrimSpace(lotID)
	for _, view := range views {
		if view.ID == lotID || view.LotNo == lotID {
			return view, nil
		}
	}
	return LotView{}, ErrNotFound
}

func (s *Service) UpsertChemLot(ctx context.Context, row ChemLot) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.ID) == "" {
		row.ID = ulid.Make().String()
	}
	if len(row.ID) != 26 || strings.TrimSpace(row.LotNo) == "" {
		return ErrPayloadInvalid
	}
	return ops.UpsertChemLot(ctx, row)
}

func (s *Service) InsertChemUse(ctx context.Context, row ChemUse) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.ID) == "" {
		row.ID = ulid.Make().String()
	}
	if len(row.ID) != 26 || strings.TrimSpace(row.LotID) == "" {
		return ErrPayloadInvalid
	}
	return ops.InsertChemUse(ctx, row)
}

func (s *Service) ListKitViews(ctx context.Context) ([]KitView, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	kits, err := ops.ListKits(ctx)
	if err != nil {
		return nil, err
	}
	items, err := ops.ListKitItems(ctx)
	if err != nil {
		return nil, err
	}
	byKit := map[string][]KitItem{}
	for _, item := range items {
		byKit[item.KitID] = append(byKit[item.KitID], item)
	}
	out := make([]KitView, 0, len(kits))
	for _, kit := range kits {
		kitItems := byKit[kit.ID]
		out = append(out, KitView{Kit: kit, Items: kitItems, Missing: KitShortage(kitItems)})
	}
	return out, nil
}

func (s *Service) UpsertKit(ctx context.Context, row Kit, items []KitItem) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.ID) == "" {
		row.ID = ulid.Make().String()
	}
	if len(row.ID) != 26 || strings.TrimSpace(row.Name) == "" {
		return ErrPayloadInvalid
	}
	if err := ops.UpsertKit(ctx, row); err != nil {
		return err
	}
	for _, item := range items {
		item.KitID = row.ID
		if err := ops.UpsertKitItem(ctx, item); err != nil {
			return err
		}
	}
	return nil
}

func (s *Service) ListParts(ctx context.Context, config string) ([]PartsStock, []AlternateMatch, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, nil, err
	}
	stock, err := ops.ListPartsStock(ctx)
	if err != nil {
		return nil, nil, err
	}
	alts, err := ops.ListAlternates(ctx)
	if err != nil {
		return nil, nil, err
	}
	qty := map[string]float64{}
	for _, row := range stock {
		qty[row.PN] = row.Qty
	}
	enriched := make([]Alternate, 0, len(alts))
	for _, alt := range alts {
		if alt.Qty == 0 {
			alt.Qty = qty[alt.PNTo]
		}
		enriched = append(enriched, alt)
	}
	return stock, FilterAlternates(enriched, config), nil
}

func (s *Service) UpsertPartsStock(ctx context.Context, row PartsStock) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.PN) == "" {
		return ErrPayloadInvalid
	}
	if strings.TrimSpace(row.Source) == "" {
		row.Source = "local"
	}
	return ops.UpsertPartsStock(ctx, row)
}

func (s *Service) UpsertAlternate(ctx context.Context, row Alternate) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	if strings.TrimSpace(row.PNFrom) == "" || strings.TrimSpace(row.PNTo) == "" {
		return ErrPayloadInvalid
	}
	return ops.UpsertAlternate(ctx, row)
}

func (s *Service) ListWorkPackages(ctx context.Context) ([]WorkPackage, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	return ops.ListWorkPackages(ctx)
}

func (s *Service) BuildWorkPackage(ctx context.Context, title string, cards, ads, mels, open []string) (WorkPackage, error) {
	ops, err := s.ops()
	if err != nil {
		return WorkPackage{}, err
	}
	pkg := AssembleWorkPackage(cards, ads, mels, open)
	pkg.ID = ulid.Make().String()
	if strings.TrimSpace(title) != "" {
		pkg.Title = strings.TrimSpace(title)
	}
	pkg.CreatedAt = s.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	if err := ops.UpsertWorkPackage(ctx, pkg); err != nil {
		return WorkPackage{}, err
	}
	if err := ops.InsertWorkPackageTasks(ctx, pkg.ID, cards); err != nil {
		return WorkPackage{}, err
	}
	return pkg, nil
}

func (s *Service) CheckConstraints(ctx context.Context) ([]ConstraintViolation, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	assignments, err := ops.ListScheduleAssignments(ctx)
	if err != nil {
		return nil, err
	}
	slots, err := ops.ListCapacitySlots(ctx)
	if err != nil {
		return nil, err
	}
	dueViews, err := s.ListDue(ctx)
	if err != nil {
		return nil, err
	}
	dues := make([]DueStatus, 0, len(dueViews))
	for _, view := range dueViews {
		dues = append(dues, view.Status)
	}
	kits, err := s.ListKitViews(ctx)
	if err != nil {
		return nil, err
	}
	var missing []string
	for _, kit := range kits {
		missing = append(missing, kit.Missing...)
	}
	stock, alts, err := s.ListParts(ctx, "")
	if err != nil {
		return nil, err
	}
	accepted := map[string]bool{}
	for _, alt := range alts {
		if alt.Accepted {
			accepted[alt.PNFrom] = true
		}
	}
	var longLead []string
	for _, row := range stock {
		if row.Qty <= 0 && !accepted[row.PN] {
			longLead = append(longLead, row.PN)
		}
	}
	rules, err := ops.ListIntervalRules(ctx)
	if err != nil {
		return nil, err
	}
	// No rules means nothing violates the dual-source rule; any rule missing a
	// source cite flips this to a C7 violation.
	hasCite := true
	for _, rule := range rules {
		if strings.TrimSpace(rule.SourceCite) == "" {
			hasCite = false
			break
		}
	}
	aog, err := ops.ListAOGTails(ctx)
	if err != nil {
		return nil, err
	}
	return CheckScheduleConstraints(ScheduleInput{
		Assignments: assignments,
		Slots:       slots,
		Dues:        dues,
		AOGTails:    aog,
		KitMissing:  missing,
		LongLeadPN:  longLead,
		HasCite:     hasCite,
		Today:       s.clock.Now(),
	}), nil
}

func (s *Service) PublishSchedule(ctx context.Context, packageID string) ([]OpsTodo, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	packageID = strings.TrimSpace(packageID)
	pkgs, err := ops.ListWorkPackages(ctx)
	if err != nil {
		return nil, err
	}
	var pkg WorkPackage
	found := false
	for _, item := range pkgs {
		if item.ID == packageID {
			pkg = item
			found = true
			break
		}
	}
	if !found {
		return nil, ErrNotFound
	}
	todos := PublishScheduleTodos(pkg)
	now := s.clock.Now().UTC().Format("2006-01-02T15:04:05Z")
	for i := range todos {
		todos[i].ID = ulid.Make().String()
		todos[i].CreatedAt = now
	}
	if err := ops.InsertOpsTodos(ctx, todos); err != nil {
		return nil, err
	}
	return todos, nil
}

func (s *Service) ListOpsTodos(ctx context.Context) ([]OpsTodo, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	return ops.ListOpsTodos(ctx)
}

func (s *Service) BulletinChain(ctx context.Context, lotID string) (BulletinChain, error) {
	ops, err := s.ops()
	if err != nil {
		return BulletinChain{}, err
	}
	uses, err := ops.ListChemUses(ctx)
	if err != nil {
		return BulletinChain{}, err
	}
	lots, err := ops.ListChemLots(ctx)
	if err != nil {
		return BulletinChain{}, err
	}
	return QualityBulletinChain(strings.TrimSpace(lotID), uses, lots), nil
}

func (s *Service) ProposeIntervalChangeDraft(ctx context.Context, taskKey, mpdCite, fleetCite string) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	ok, _ := ProposeIntervalChange(mpdCite, fleetCite)
	if !ok {
		return ErrPayloadInvalid
	}
	if strings.TrimSpace(taskKey) == "" {
		return ErrPayloadInvalid
	}
	return ops.InsertIntervalChangeDraft(ctx, taskKey, mpdCite, fleetCite, s.clock.Now().UTC().Format("2006-01-02T15:04:05Z"))
}

const opsTimeLayout = "2006-01-02T15:04:05Z"

// RecordUtilization appends a usage tick for a scope then recomputes every due
// item so used_value reflects the ledger. It never invents zeros.
func (s *Service) RecordUtilization(ctx context.Context, scopeID string, hours, cycles, battery float64) ([]DueView, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	scopeID = strings.TrimSpace(scopeID)
	if scopeID == "" || len(scopeID) > 64 || hours < 0 || cycles < 0 || battery < 0 {
		return nil, ErrPayloadInvalid
	}
	if hours == 0 && cycles == 0 && battery == 0 {
		return nil, ErrPayloadInvalid
	}
	ev := UtilizationEvent{
		ID: ulid.Make().String(), ScopeID: scopeID,
		Hours: hours, Cycles: cycles, BatteryCycles: battery,
		CreatedAt: s.clock.Now().UTC().Format(opsTimeLayout),
	}
	if err := ops.RecordUtilization(ctx, ev); err != nil {
		return nil, err
	}
	return s.RecomputeDue(ctx)
}

// RecomputeDue folds utilization totals into usage-based due items, persists
// any change, and returns the evaluated views.
func (s *Service) RecomputeDue(ctx context.Context) ([]DueView, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	items, err := ops.ListDueItems(ctx)
	if err != nil {
		return nil, err
	}
	events, err := ops.ListUtilizationEvents(ctx)
	if err != nil {
		return nil, err
	}
	totals := SumUtilization(events)
	now := s.clock.Now()
	nowStr := now.UTC().Format(opsTimeLayout)
	out := make([]DueView, 0, len(items))
	for _, item := range items {
		recomputed := RecomputeUsed(item, totals)
		if recomputed.UsedMissing != item.UsedMissing || recomputed.UsedValue != item.UsedValue {
			recomputed.UpdatedAt = nowStr
			if err := ops.UpsertDueItem(ctx, recomputed); err != nil {
				return nil, err
			}
		}
		out = append(out, DueView{DueItem: recomputed, Status: EvaluateDue(recomputed, now)})
	}
	return out, nil
}

// ReturnTool closes the open loan and returns the tool to ready.
func (s *Service) ReturnTool(ctx context.Context, toolID string) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	toolID = strings.TrimSpace(toolID)
	if len(toolID) != 26 {
		return ErrPayloadInvalid
	}
	items, err := ops.ListTools(ctx)
	if err != nil {
		return err
	}
	var tool Tool
	found := false
	for _, item := range items {
		if item.ID == toolID {
			tool = item
			found = true
			break
		}
	}
	if !found {
		return ErrNotFound
	}
	now := s.clock.Now().UTC().Format(opsTimeLayout)
	if err := ops.CloseOpenToolLoan(ctx, toolID, now); err != nil {
		return err
	}
	tool.Holder = ""
	tool.Status = "ready"
	tool.UpdatedAt = now
	return ops.UpsertTool(ctx, tool)
}

// UpsertScheduleAssignment records one maintenance window assignment.
func (s *Service) UpsertScheduleAssignment(ctx context.Context, row ScheduleAssignment) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	row.TailNo = strings.TrimSpace(row.TailNo)
	row.CheckName = strings.TrimSpace(row.CheckName)
	if row.TailNo == "" || len(row.TailNo) > 32 || row.CheckName == "" || len(row.CheckName) > 64 {
		return ErrPayloadInvalid
	}
	if row.Hours < 0 {
		return ErrPayloadInvalid
	}
	return ops.InsertScheduleAssignment(ctx, ulid.Make().String(), row)
}

// UpsertCapacitySlot sets the available hours for one skill group.
func (s *Service) UpsertCapacitySlot(ctx context.Context, skill string, hours float64) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	skill = strings.TrimSpace(skill)
	if skill == "" || len(skill) > 64 || hours < 0 {
		return ErrPayloadInvalid
	}
	return ops.UpsertCapacitySlot(ctx, skill, hours)
}

// UpsertIntervalRule sets the interval for a task. Numbers come only from here;
// a missing source_cite trips the C7 constraint but is not rejected on entry.
func (s *Service) UpsertIntervalRule(ctx context.Context, row IntervalRule) error {
	ops, err := s.ops()
	if err != nil {
		return err
	}
	row.TaskKey = strings.TrimSpace(row.TaskKey)
	row.Unit = strings.TrimSpace(row.Unit)
	if row.TaskKey == "" || len(row.TaskKey) > 64 || row.Unit == "" || len(row.Unit) > 16 || row.IntervalValue <= 0 {
		return ErrPayloadInvalid
	}
	if strings.TrimSpace(row.ID) == "" {
		row.ID = ulid.Make().String()
	}
	if len(row.ID) != 26 {
		return ErrPayloadInvalid
	}
	return ops.UpsertIntervalRule(ctx, row)
}

// ListIntervalRules exposes the interval rules for the plan rail.
func (s *Service) ListIntervalRules(ctx context.Context) ([]IntervalRule, error) {
	ops, err := s.ops()
	if err != nil {
		return nil, err
	}
	return ops.ListIntervalRules(ctx)
}

// IssueChemical splits a child lot from a parent and records the use against
// the child. Parent qty is reduced; the engine never invents stock.
func (s *Service) IssueChemical(ctx context.Context, parentLotID string, qty float64, tailNo, wo, tech string) (ChemLot, error) {
	ops, err := s.ops()
	if err != nil {
		return ChemLot{}, err
	}
	parentLotID = strings.TrimSpace(parentLotID)
	if len(parentLotID) != 26 || qty <= 0 {
		return ChemLot{}, ErrPayloadInvalid
	}
	lots, err := ops.ListChemLots(ctx)
	if err != nil {
		return ChemLot{}, err
	}
	var parent ChemLot
	found := false
	for _, lot := range lots {
		if lot.ID == parentLotID {
			parent = lot
			found = true
			break
		}
	}
	if !found {
		return ChemLot{}, ErrNotFound
	}
	if parent.Qty < qty {
		return ChemLot{}, ErrPayloadInvalid
	}
	child := ChemLot{
		ID: ulid.Make().String(), LotNo: parent.LotNo + "-I",
		ParentLotID: parent.ID, Qty: qty, Expires: parent.Expires, SDSDoc: parent.SDSDoc,
	}
	parent.Qty -= qty
	if err := ops.UpsertChemLot(ctx, parent); err != nil {
		return ChemLot{}, err
	}
	if err := ops.UpsertChemLot(ctx, child); err != nil {
		return ChemLot{}, err
	}
	if err := s.InsertChemUse(ctx, ChemUse{LotID: child.ID, TailNo: strings.TrimSpace(tailNo), WO: strings.TrimSpace(wo), Tech: strings.TrimSpace(tech)}); err != nil {
		return ChemLot{}, err
	}
	return child, nil
}

// AddPartsTodo persists a kit shortage as a parts_request. A second click on
// the same open kit is a no-op so refresh stays honest.
func (s *Service) AddPartsTodo(ctx context.Context, kitID, detail string) (OpsTodo, error) {
	ops, err := s.ops()
	if err != nil {
		return OpsTodo{}, err
	}
	kitID = strings.TrimSpace(kitID)
	if kitID == "" || len(kitID) > 64 {
		return OpsTodo{}, ErrPayloadInvalid
	}
	existing, err := ops.ListOpsTodos(ctx)
	if err != nil {
		return OpsTodo{}, err
	}
	for _, row := range existing {
		if row.Kind == "parts_request" && row.Ref == kitID && row.Status == "open" {
			return row, nil
		}
	}
	row := OpsTodo{
		ID: ulid.Make().String(), Kind: "parts_request", Ref: kitID,
		Status: "open", Detail: clip(detail, 256),
		CreatedAt: s.clock.Now().UTC().Format(opsTimeLayout),
	}
	if err := ops.InsertOpsTodos(ctx, []OpsTodo{row}); err != nil {
		return OpsTodo{}, err
	}
	return row, nil
}
