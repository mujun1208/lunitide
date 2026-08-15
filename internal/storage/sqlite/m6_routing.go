// Complexity routing and synthesis persistence (migration 0053).
// Decisions are (inputDigest, routerVersion)-unique; manifests, bundles
// and synthesis records are digest-pinned and insert-only — the frozen
// contract has no update path.
package sqlite

import (
	"database/sql"
	"errors"

	"github.com/lunitide/lunitide/internal/domain/m6supply"
)

// ── ComplexityDecision ──────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutM6ComplexityDecision(d m6supply.ComplexityDecision) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_complexity_decision
		(id,session_id,input_digest,router_version,tier,routed_path,reason_codes,confidence,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		d.ID, d.SessionID, d.InputDigest, d.RouterVersion, d.Tier, d.RoutedPath, d.ReasonCodes, d.Confidence, rfc(d.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) FindM6ComplexityDecision(inputDigest, routerVersion string) (m6supply.ComplexityDecision, error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT id,session_id,input_digest,router_version,tier,routed_path,reason_codes,confidence,created_at FROM m6_complexity_decision WHERE input_digest=? AND router_version=?`, inputDigest, routerVersion)
	var d m6supply.ComplexityDecision
	var created string
	err := row.Scan(&d.ID, &d.SessionID, &d.InputDigest, &d.RouterVersion, &d.Tier, &d.RoutedPath, &d.ReasonCodes, &d.Confidence, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return d, m6supply.ErrNotFound
	}
	if err != nil {
		return d, err
	}
	if d.CreatedAt, err = parseRFC(created); err != nil {
		return d, err
	}
	return d, nil
}

func (t *agentRuntimeTx) ListM6ComplexityDecisions(sessionID string, limit int) ([]m6supply.ComplexityDecision, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,session_id,input_digest,router_version,tier,routed_path,reason_codes,confidence,created_at FROM m6_complexity_decision WHERE session_id=? ORDER BY created_at DESC LIMIT ?`, sessionID, limit)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.ComplexityDecision
	for rows.Next() {
		var d m6supply.ComplexityDecision
		var created string
		if err := rows.Scan(&d.ID, &d.SessionID, &d.InputDigest, &d.RouterVersion, &d.Tier, &d.RoutedPath, &d.ReasonCodes, &d.Confidence, &created); err != nil {
			return nil, t.fail(err)
		}
		if d.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, d)
	}
	return out, rows.Err()
}

// ── ChildContextManifest ────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutM6ChildManifest(m m6supply.ChildContextManifest) error {
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_child_manifest
		(id,delegation_id,manifest_digest,task_scope,locked_inputs,budget_json,capabilities,created_at)
		VALUES(?,?,?,?,?,?,?,?)`,
		m.ID, m.DelegationID, m.ManifestDigest, m.TaskScope, m.LockedInputs, m.BudgetJSON, m.Capabilities, rfc(m.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) GetM6ChildManifestByDelegation(delegationID string) (m6supply.ChildContextManifest, error) {
	row := t.tx.QueryRowContext(t.ctx, `SELECT id,delegation_id,manifest_digest,task_scope,locked_inputs,budget_json,capabilities,created_at FROM m6_child_manifest WHERE delegation_id=?`, delegationID)
	var m m6supply.ChildContextManifest
	var created string
	err := row.Scan(&m.ID, &m.DelegationID, &m.ManifestDigest, &m.TaskScope, &m.LockedInputs, &m.BudgetJSON, &m.Capabilities, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return m, m6supply.ErrNotFound
	}
	if err != nil {
		return m, err
	}
	if m.CreatedAt, err = parseRFC(created); err != nil {
		return m, err
	}
	return m, nil
}

// ── ResultBundle ────────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutM6ResultBundle(b m6supply.ResultBundle) error {
	var patchDigest any
	if b.PatchDigest != "" {
		patchDigest = b.PatchDigest
	}
	var riskNotes any
	if b.RiskNotes != "" {
		riskNotes = b.RiskNotes
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_result_bundle
		(id,delegation_id,child_id,attempt,base_head,claims,patch_digest,test_evidence,usage,risk_notes,result_digest,created_at)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?)`,
		b.ID, b.DelegationID, b.ChildID, b.Attempt, b.BaseHead, b.Claims, patchDigest, b.TestEvidence, b.Usage, riskNotes, b.ResultDigest, rfc(b.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListM6ResultBundles(delegationID string) ([]m6supply.ResultBundle, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,delegation_id,child_id,attempt,base_head,claims,patch_digest,test_evidence,usage,risk_notes,result_digest,created_at FROM m6_result_bundle WHERE delegation_id=? ORDER BY attempt`, delegationID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.ResultBundle
	for rows.Next() {
		var b m6supply.ResultBundle
		var patchDigest, riskNotes sql.NullString
		var created string
		if err := rows.Scan(&b.ID, &b.DelegationID, &b.ChildID, &b.Attempt, &b.BaseHead, &b.Claims, &patchDigest, &b.TestEvidence, &b.Usage, &riskNotes, &b.ResultDigest, &created); err != nil {
			return nil, t.fail(err)
		}
		b.PatchDigest = patchDigest.String
		b.RiskNotes = riskNotes.String
		if b.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, b)
	}
	return out, rows.Err()
}

// ── SynthesisRecord ─────────────────────────────────────────────────────────

func (t *agentRuntimeTx) PutM6SynthesisRecord(r m6supply.SynthesisRecord) error {
	var barrierID any
	if r.BarrierID != "" {
		barrierID = r.BarrierID
	}
	_, err := t.tx.ExecContext(t.ctx, `INSERT INTO m6_synthesis_record
		(id,root_id,barrier_id,synthesis_digest,consistent,conflicts,missing_evidence,adoption_reasons,created_at)
		VALUES(?,?,?,?,?,?,?,?,?)`,
		r.ID, r.RootID, barrierID, r.SynthesisDigest, r.Consistent, r.Conflicts, r.MissingEvidence, r.AdoptionReasons, rfc(r.CreatedAt))
	return t.fail(err)
}

func (t *agentRuntimeTx) ListM6SynthesisRecords(rootID string) ([]m6supply.SynthesisRecord, error) {
	rows, err := t.tx.QueryContext(t.ctx, `SELECT id,root_id,barrier_id,synthesis_digest,consistent,conflicts,missing_evidence,adoption_reasons,created_at FROM m6_synthesis_record WHERE root_id=? ORDER BY created_at`, rootID)
	if err != nil {
		return nil, t.fail(err)
	}
	defer rows.Close()
	var out []m6supply.SynthesisRecord
	for rows.Next() {
		var r m6supply.SynthesisRecord
		var barrierID sql.NullString
		var created string
		if err := rows.Scan(&r.ID, &r.RootID, &barrierID, &r.SynthesisDigest, &r.Consistent, &r.Conflicts, &r.MissingEvidence, &r.AdoptionReasons, &created); err != nil {
			return nil, t.fail(err)
		}
		r.BarrierID = barrierID.String
		if r.CreatedAt, err = parseRFC(created); err != nil {
			return nil, t.fail(err)
		}
		out = append(out, r)
	}
	return out, rows.Err()
}
