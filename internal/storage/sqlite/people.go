package sqlite

import (
	"context"
	"database/sql"
	"errors"
	"sort"

	"github.com/lunitide/lunitide/internal/people"
)

const contactCols = `subject_id, nickname, avatar, status, department, title, org_name, bio, public_key, pairing_hash, trust_state, host_addr, last_seen_at, created_at, updated_at, remark, blocked`

func (s *Store) ListContacts(ctx context.Context) ([]people.Contact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT `+contactCols+` FROM people_contacts ORDER BY CASE trust_state WHEN 'self' THEN 0 WHEN 'trusted' THEN 1 ELSE 2 END, department, nickname, subject_id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []people.Contact
	for rows.Next() {
		c, scanErr := scanContact(rows)
		if scanErr != nil {
			return nil, scanErr
		}
		items = append(items, c)
	}
	return items, rows.Err()
}

func (s *Store) GetContact(ctx context.Context, subjectID string) (people.Contact, error) {
	row := s.db.QueryRowContext(ctx, `SELECT `+contactCols+` FROM people_contacts WHERE subject_id=?`, subjectID)
	c, err := scanContact(row)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Contact{}, people.ErrNotFound
	}
	return c, err
}

func (s *Store) UpsertContact(ctx context.Context, c people.Contact) error {
	blocked := 0
	if c.Blocked {
		blocked = 1
	}
	_, err := s.db.ExecContext(ctx, `INSERT INTO people_contacts(subject_id, nickname, avatar, status, department, title, org_name, bio, public_key, pairing_hash, trust_state, host_addr, last_seen_at, created_at, updated_at, remark, blocked)
		VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)
		ON CONFLICT(subject_id) DO UPDATE SET nickname=excluded.nickname, avatar=excluded.avatar, status=excluded.status, department=excluded.department, title=excluded.title, org_name=excluded.org_name, bio=excluded.bio, public_key=excluded.public_key, pairing_hash=excluded.pairing_hash, trust_state=excluded.trust_state, host_addr=excluded.host_addr, last_seen_at=excluded.last_seen_at, updated_at=excluded.updated_at, remark=excluded.remark, blocked=excluded.blocked`,
		c.SubjectID, c.Nickname, c.Avatar, c.Status, c.Department, c.Title, c.OrgName, c.Bio, c.PublicKey, c.PairingHash, c.TrustState, c.HostAddr, c.LastSeenAt, c.CreatedAt, c.UpdatedAt, c.Remark, blocked)
	return err
}

func (s *Store) ListThreads(ctx context.Context, selfID string) ([]people.Thread, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT t.thread_id FROM people_threads t JOIN people_thread_members m ON m.thread_id=t.thread_id WHERE m.subject_id=? ORDER BY t.updated_at DESC, t.thread_id DESC`, selfID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	out := make([]people.Thread, 0, len(ids))
	for _, id := range ids {
		t, err := s.GetThread(ctx, id)
		if err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, nil
}

func (s *Store) GetThread(ctx context.Context, threadID string) (people.Thread, error) {
	var t people.Thread
	err := s.db.QueryRowContext(ctx, `SELECT thread_id, kind, title, owner_subject_id, created_at, updated_at FROM people_threads WHERE thread_id=?`, threadID).Scan(&t.ThreadID, &t.Kind, &t.Title, &t.OwnerID, &t.CreatedAt, &t.UpdatedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return people.Thread{}, people.ErrNotFound
	}
	if err != nil {
		return people.Thread{}, err
	}
	members, err := s.threadMembers(ctx, threadID)
	if err != nil {
		return people.Thread{}, err
	}
	t.Members = members
	last, err := s.lastMessage(ctx, threadID)
	if err != nil {
		return people.Thread{}, err
	}
	t.LastMessage = last
	return t, nil
}

func (s *Store) FindDirectThread(ctx context.Context, a, b string) (people.Thread, bool, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT thread_id FROM people_threads WHERE kind='direct'`)
	if err != nil {
		return people.Thread{}, false, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return people.Thread{}, false, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return people.Thread{}, false, err
	}
	for _, id := range ids {
		members, err := s.memberIDs(ctx, id)
		if err != nil {
			return people.Thread{}, false, err
		}
		if isDirectPair(members, a, b) {
			t, err := s.GetThread(ctx, id)
			return t, err == nil, err
		}
	}
	return people.Thread{}, false, nil
}

func (s *Store) InsertThread(ctx context.Context, t people.Thread, memberIDs []string, ownerID string) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO people_threads(thread_id, kind, title, owner_subject_id, created_at, updated_at) VALUES(?,?,?,?,?,?)`, t.ThreadID, t.Kind, t.Title, t.OwnerID, t.CreatedAt, t.UpdatedAt); err != nil {
		return err
	}
	for _, id := range memberIDs {
		role := "member"
		if id == ownerID {
			role = "owner"
		}
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO people_thread_members(thread_id, subject_id, role, joined_at) VALUES(?,?,?,?)`, t.ThreadID, id, role, t.CreatedAt); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) ListPeopleMessages(ctx context.Context, threadID string, limit int) ([]people.Message, error) {
	if limit < 1 {
		limit = 200
	}
	rows, err := s.db.QueryContext(ctx, `SELECT m.message_id, m.thread_id, m.sender_subject_id, m.kind, m.body, m.file_name, m.file_mime, m.file_size, m.file_sha256, m.created_at, COALESCE(o.offer_id,''), COALESCE(o.status,''), COALESCE(NULLIF(o.dest_path,''), COALESCE(o.staging_path,''))
		FROM people_messages m LEFT JOIN people_file_offers o ON o.message_id=m.message_id
		WHERE m.thread_id=? ORDER BY m.created_at ASC, m.message_id ASC LIMIT ?`, threadID, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []people.Message
	for rows.Next() {
		var m people.Message
		if err := rows.Scan(&m.MessageID, &m.ThreadID, &m.SenderID, &m.Kind, &m.Body, &m.FileName, &m.FileMIME, &m.FileSize, &m.FileSHA256, &m.CreatedAt, &m.OfferID, &m.OfferStatus, &m.DestPath); err != nil {
			return nil, err
		}
		items = append(items, m)
	}
	return items, rows.Err()
}

func (s *Store) HasPeopleMessage(ctx context.Context, messageID string) (bool, error) {
	var n int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM people_messages WHERE message_id=?`, messageID).Scan(&n)
	return n > 0, err
}

func (s *Store) InsertMessage(ctx context.Context, m people.Message, offer *people.FileOffer) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	res, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO people_messages(message_id, thread_id, sender_subject_id, kind, body, file_name, file_mime, file_size, file_sha256, created_at) VALUES(?,?,?,?,?,?,?,?,?,?)`,
		m.MessageID, m.ThreadID, m.SenderID, m.Kind, m.Body, m.FileName, m.FileMIME, m.FileSize, m.FileSHA256, m.CreatedAt)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return tx.Commit()
	}
	if _, err := tx.ExecContext(ctx, `UPDATE people_threads SET updated_at=? WHERE thread_id=?`, m.CreatedAt, m.ThreadID); err != nil {
		return err
	}
	if offer != nil {
		if _, err := tx.ExecContext(ctx, `INSERT OR IGNORE INTO people_file_offers(offer_id, message_id, thread_id, from_subject_id, to_subject_id, status, file_name, file_mime, file_size, file_sha256, staging_path, dest_path, created_at, decided_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`,
			offer.OfferID, offer.MessageID, offer.ThreadID, offer.FromID, offer.ToID, offer.Status, offer.FileName, offer.FileMIME, offer.FileSize, offer.FileSHA256, offer.StagingPath, "", offer.CreatedAt, ""); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) GetOffer(ctx context.Context, offerID string) (people.FileOffer, error) {
	return s.scanOffer(ctx, `SELECT offer_id, message_id, thread_id, from_subject_id, to_subject_id, status, file_name, file_mime, file_size, file_sha256, staging_path, dest_path, created_at, decided_at FROM people_file_offers WHERE offer_id=?`, offerID)
}

func (s *Store) GetOfferByMessage(ctx context.Context, messageID string) (people.FileOffer, error) {
	return s.scanOffer(ctx, `SELECT offer_id, message_id, thread_id, from_subject_id, to_subject_id, status, file_name, file_mime, file_size, file_sha256, staging_path, dest_path, created_at, decided_at FROM people_file_offers WHERE message_id=?`, messageID)
}

func (s *Store) scanOffer(ctx context.Context, query, id string) (people.FileOffer, error) {
	var o people.FileOffer
	err := s.db.QueryRowContext(ctx, query, id).Scan(
		&o.OfferID, &o.MessageID, &o.ThreadID, &o.FromID, &o.ToID, &o.Status, &o.FileName, &o.FileMIME, &o.FileSize, &o.FileSHA256, &o.StagingPath, &o.DestPath, &o.CreatedAt, &o.DecidedAt)
	if errors.Is(err, sql.ErrNoRows) {
		return people.FileOffer{}, people.ErrNotFound
	}
	return o, err
}

func (s *Store) DecideOffer(ctx context.Context, offerID, status, destPath, decidedAt string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE people_file_offers SET status=?, dest_path=?, decided_at=? WHERE offer_id=?`, status, destPath, decidedAt, offerID)
	return err
}

func (s *Store) MarkThreadRead(ctx context.Context, threadID, subjectID, at string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE people_thread_members SET last_read_at=? WHERE thread_id=? AND subject_id=?`, at, threadID, subjectID)
	return err
}

func (s *Store) CountUnread(ctx context.Context, threadID, subjectID string) (int, error) {
	var lastRead string
	err := s.db.QueryRowContext(ctx, `SELECT last_read_at FROM people_thread_members WHERE thread_id=? AND subject_id=?`, threadID, subjectID).Scan(&lastRead)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	var n int
	if lastRead == "" {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM people_messages WHERE thread_id=? AND sender_subject_id<>?`, threadID, subjectID).Scan(&n)
	} else {
		err = s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM people_messages WHERE thread_id=? AND sender_subject_id<>? AND created_at>?`, threadID, subjectID, lastRead).Scan(&n)
	}
	return n, err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanContact(row rowScanner) (people.Contact, error) {
	var c people.Contact
	var blocked int
	err := row.Scan(&c.SubjectID, &c.Nickname, &c.Avatar, &c.Status, &c.Department, &c.Title, &c.OrgName, &c.Bio, &c.PublicKey, &c.PairingHash, &c.TrustState, &c.HostAddr, &c.LastSeenAt, &c.CreatedAt, &c.UpdatedAt, &c.Remark, &blocked)
	c.Blocked = blocked != 0
	return c, err
}

func (s *Store) lastMessage(ctx context.Context, threadID string) (*people.Message, error) {
	var m people.Message
	err := s.db.QueryRowContext(ctx, `SELECT m.message_id, m.thread_id, m.sender_subject_id, m.kind, m.body, m.file_name, m.file_mime, m.file_size, m.file_sha256, m.created_at, COALESCE(o.offer_id,''), COALESCE(o.status,''), COALESCE(NULLIF(o.dest_path,''), COALESCE(o.staging_path,''))
		FROM people_messages m LEFT JOIN people_file_offers o ON o.message_id=m.message_id
		WHERE m.thread_id=? ORDER BY m.created_at DESC, m.message_id DESC LIMIT 1`, threadID).Scan(
		&m.MessageID, &m.ThreadID, &m.SenderID, &m.Kind, &m.Body, &m.FileName, &m.FileMIME, &m.FileSize, &m.FileSHA256, &m.CreatedAt, &m.OfferID, &m.OfferStatus, &m.DestPath)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) threadMembers(ctx context.Context, threadID string) ([]people.Contact, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT c.subject_id, c.nickname, c.avatar, c.status, c.department, c.title, c.org_name, c.bio, c.public_key, c.pairing_hash, c.trust_state, c.host_addr, c.last_seen_at, c.created_at, c.updated_at, c.remark, c.blocked, m.last_read_at
		FROM people_thread_members m JOIN people_contacts c ON c.subject_id=m.subject_id WHERE m.thread_id=? ORDER BY m.role DESC, c.nickname, c.subject_id`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var items []people.Contact
	for rows.Next() {
		var c people.Contact
		var blocked int
		if err := rows.Scan(&c.SubjectID, &c.Nickname, &c.Avatar, &c.Status, &c.Department, &c.Title, &c.OrgName, &c.Bio, &c.PublicKey, &c.PairingHash, &c.TrustState, &c.HostAddr, &c.LastSeenAt, &c.CreatedAt, &c.UpdatedAt, &c.Remark, &blocked, &c.LastReadAt); err != nil {
			return nil, err
		}
		c.Blocked = blocked != 0
		c.PairingHash = ""
		items = append(items, c)
	}
	return items, rows.Err()
}

func (s *Store) memberIDs(ctx context.Context, threadID string) ([]string, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT subject_id FROM people_thread_members WHERE thread_id=?`, threadID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var ids []string
	for rows.Next() {
		var id string
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	return ids, rows.Err()
}

func (s *Store) ThreadSession(ctx context.Context, threadID string) (string, bool, error) {
	var sessionID string
	err := s.db.QueryRowContext(ctx, `SELECT session_id FROM people_thread_session WHERE thread_id=?`, threadID).Scan(&sessionID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return sessionID, true, nil
}

func (s *Store) BindThreadSession(ctx context.Context, threadID, sessionID, createdAt string) error {
	_, err := s.db.ExecContext(ctx, `INSERT INTO people_thread_session(thread_id, session_id, created_at) VALUES(?,?,?)
		ON CONFLICT(thread_id) DO NOTHING`, threadID, sessionID, createdAt)
	return err
}

func (s *Store) ClearThreadSession(ctx context.Context, threadID string) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM people_thread_session WHERE thread_id=?`, threadID)
	return err
}

func isDirectPair(members []string, a, b string) bool {
	if a == b {
		return len(members) == 1 && members[0] == a
	}
	if len(members) != 2 {
		return false
	}
	sort.Strings(members)
	pair := []string{a, b}
	sort.Strings(pair)
	return members[0] == pair[0] && members[1] == pair[1]
}
