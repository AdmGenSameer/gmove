package database

import (
	"database/sql"
	"fmt"
	"time"

	"github.com/AdmGenSameer/gmove/internal/constants"
)

// Repository provides structured database queries.
type Repository struct {
	db *DB
}

func NewRepository(db *DB) *Repository {
	return &Repository{db: db}
}

// CreateOperation inserts a new operation record.
func (r *Repository) CreateOperation(op *Operation) (int64, error) {
	query := `
	INSERT INTO operations (
		started_at, status, source, destination, total_items, total_files, total_bytes, dry_run, notes
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`

	res, err := r.db.Exec(query,
		op.StartedAt,
		string(op.Status),
		op.Source,
		op.Destination,
		op.TotalItems,
		op.TotalFiles,
		op.TotalBytes,
		op.DryRun,
		op.Notes,
	)
	if err != nil {
		return 0, fmt.Errorf("failed to insert operation: %w", err)
	}

	id, err := res.LastInsertId()
	if err != nil {
		return 0, err
	}
	op.ID = id
	return id, nil
}

// UpdateOperationStatus updates status and optional completion timestamp.
func (r *Repository) UpdateOperationStatus(opID int64, status constants.Status, completedAt *time.Time) error {
	query := `UPDATE operations SET status = ?, completed_at = ? WHERE id = ?`
	_, err := r.db.Exec(query, string(status), completedAt, opID)
	return err
}

// GetOperation retrieves an operation by ID.
func (r *Repository) GetOperation(opID int64) (*Operation, error) {
	query := `
	SELECT id, started_at, completed_at, status, source, destination, total_items, total_files, total_bytes, dry_run, notes
	FROM operations WHERE id = ?`

	row := r.db.QueryRow(query, opID)
	op := &Operation{}
	var statusStr string
	var notes sql.NullString
	var completedAt sql.NullTime

	err := row.Scan(
		&op.ID,
		&op.StartedAt,
		&completedAt,
		&statusStr,
		&op.Source,
		&op.Destination,
		&op.TotalItems,
		&op.TotalFiles,
		&op.TotalBytes,
		&op.DryRun,
		&notes,
	)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, fmt.Errorf("operation #%d not found", opID)
		}
		return nil, err
	}

	op.Status = constants.Status(statusStr)
	if completedAt.Valid {
		op.CompletedAt = &completedAt.Time
	}
	if notes.Valid {
		op.Notes = notes.String
	}

	return op, nil
}

// GetLatestOperation retrieves the most recent operation.
func (r *Repository) GetLatestOperation() (*Operation, error) {
	var opID int64
	err := r.db.QueryRow(`SELECT id FROM operations ORDER BY id DESC LIMIT 1`).Scan(&opID)
	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return r.GetOperation(opID)
}

// GetIncompleteOperation checks for an operation in RUNNING, PENDING, or INTERRUPTED status.
func (r *Repository) GetIncompleteOperation() (*Operation, error) {
	var opID int64
	query := `
	SELECT id FROM operations 
	WHERE status IN (?, ?, ?) 
	ORDER BY id DESC LIMIT 1`

	err := r.db.QueryRow(query,
		string(constants.StatusRunning),
		string(constants.StatusPending),
		string(constants.StatusInterrupted),
	).Scan(&opID)

	if err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	return r.GetOperation(opID)
}

// ListOperations lists recent operations up to limit.
func (r *Repository) ListOperations(limit int) ([]*Operation, error) {
	if limit <= 0 {
		limit = 20
	}

	query := `
	SELECT id, started_at, completed_at, status, source, destination, total_items, total_files, total_bytes, dry_run, notes
	FROM operations ORDER BY id DESC LIMIT ?`

	rows, err := r.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ops []*Operation
	for rows.Next() {
		op := &Operation{}
		var statusStr string
		var notes sql.NullString
		var completedAt sql.NullTime

		if err := rows.Scan(
			&op.ID,
			&op.StartedAt,
			&completedAt,
			&statusStr,
			&op.Source,
			&op.Destination,
			&op.TotalItems,
			&op.TotalFiles,
			&op.TotalBytes,
			&op.DryRun,
			&notes,
		); err != nil {
			return nil, err
		}

		op.Status = constants.Status(statusStr)
		if completedAt.Valid {
			op.CompletedAt = &completedAt.Time
		}
		if notes.Valid {
			op.Notes = notes.String
		}

		ops = append(ops, op)
	}

	return ops, rows.Err()
}

// AddTransferItems inserts a batch of transfer items within a transaction.
func (r *Repository) AddTransferItems(items []*TransferItem) error {
	tx, err := r.db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
	INSERT INTO transfer_items (
		operation_id, name, is_directory, relative_path, source_abs_path, destination_rel_path,
		size_bytes, mtime_epoch, inode, device_id, status, verification_status
	) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()

	for _, item := range items {
		res, err := stmt.Exec(
			item.OperationID,
			item.Name,
			item.IsDirectory,
			item.RelativePath,
			item.SourceAbsPath,
			item.DestinationRelPath,
			item.SizeBytes,
			item.MtimeEpoch,
			item.Inode,
			item.DeviceID,
			string(item.Status),
			string(item.VerificationStatus),
		)
		if err != nil {
			return fmt.Errorf("failed to insert item %s: %w", item.RelativePath, err)
		}
		id, err := res.LastInsertId()
		if err == nil {
			item.ID = id
		}
	}

	return tx.Commit()
}

// GetTransferItems retrieves all items for an operation.
func (r *Repository) GetTransferItems(opID int64) ([]*TransferItem, error) {
	query := `
	SELECT id, operation_id, name, is_directory, relative_path, source_abs_path, destination_rel_path,
	       size_bytes, mtime_epoch, inode, device_id, status, verification_status,
	       source_hash, remote_hash, error, transferred_at, verified_at, deleted_at
	FROM transfer_items WHERE operation_id = ? ORDER BY id ASC`

	rows, err := r.db.Query(query, opID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*TransferItem
	for rows.Next() {
		item, err := scanTransferItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}

	return items, rows.Err()
}

// GetTransferItem retrieves a single item by ID.
func (r *Repository) GetTransferItem(itemID int64) (*TransferItem, error) {
	query := `
	SELECT id, operation_id, name, is_directory, relative_path, source_abs_path, destination_rel_path,
	       size_bytes, mtime_epoch, inode, device_id, status, verification_status,
	       source_hash, remote_hash, error, transferred_at, verified_at, deleted_at
	FROM transfer_items WHERE id = ?`

	row := r.db.QueryRow(query, itemID)
	return scanTransferItem(row)
}

// GetFailedItems retrieves all items that failed for an operation.
func (r *Repository) GetFailedItems(opID int64) ([]*TransferItem, error) {
	query := `
	SELECT id, operation_id, name, is_directory, relative_path, source_abs_path, destination_rel_path,
	       size_bytes, mtime_epoch, inode, device_id, status, verification_status,
	       source_hash, remote_hash, error, transferred_at, verified_at, deleted_at
	FROM transfer_items 
	WHERE operation_id = ? AND (status = ? OR verification_status = ?)
	ORDER BY id ASC`

	rows, err := r.db.Query(query, opID, string(constants.StatusFailed), string(constants.VerifError))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*TransferItem
	for rows.Next() {
		item, err := scanTransferItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// GetVerifiedItems retrieves items with status VERIFIED for an operation.
func (r *Repository) GetVerifiedItems(opID int64) ([]*TransferItem, error) {
	query := `
	SELECT id, operation_id, name, is_directory, relative_path, source_abs_path, destination_rel_path,
	       size_bytes, mtime_epoch, inode, device_id, status, verification_status,
	       source_hash, remote_hash, error, transferred_at, verified_at, deleted_at
	FROM transfer_items 
	WHERE operation_id = ? AND status = ?
	ORDER BY id ASC`

	rows, err := r.db.Query(query, opID, string(constants.StatusVerified))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var items []*TransferItem
	for rows.Next() {
		item, err := scanTransferItem(rows)
		if err != nil {
			return nil, err
		}
		items = append(items, item)
	}
	return items, rows.Err()
}

// UpdateItemStatus updates the status and error string of an item.
func (r *Repository) UpdateItemStatus(itemID int64, status constants.Status, vStatus constants.VerificationStatus, errStr string) error {
	query := `UPDATE transfer_items SET status = ?, verification_status = ?, error = ? WHERE id = ?`
	_, err := r.db.Exec(query, string(status), string(vStatus), errStr, itemID)
	return err
}

// UpdateItemTransferred updates status to TRANSFERRED.
func (r *Repository) UpdateItemTransferred(itemID int64, t time.Time) error {
	query := `UPDATE transfer_items SET status = ?, transferred_at = ? WHERE id = ?`
	_, err := r.db.Exec(query, string(constants.StatusTransferred), t, itemID)
	return err
}

// UpdateItemVerified marks an item as VERIFIED with hashes.
func (r *Repository) UpdateItemVerified(itemID int64, t time.Time, srcHash, remoteHash string) error {
	query := `
	UPDATE transfer_items 
	SET status = ?, verification_status = ?, verified_at = ?, source_hash = ?, remote_hash = ?, error = ''
	WHERE id = ?`
	_, err := r.db.Exec(query, string(constants.StatusVerified), string(constants.VerifMatched), t, srcHash, remoteHash, itemID)
	return err
}

// UpdateItemDeleted marks an item as DELETED.
func (r *Repository) UpdateItemDeleted(itemID int64, t time.Time) error {
	query := `UPDATE transfer_items SET status = ?, deleted_at = ? WHERE id = ?`
	_, err := r.db.Exec(query, string(constants.StatusDeleted), t, itemID)
	return err
}

// LogEvent records an audit event.
func (r *Repository) LogEvent(opID *int64, level, message, details string) error {
	query := `INSERT INTO events (operation_id, timestamp, level, message, details) VALUES (?, ?, ?, ?, ?)`
	_, err := r.db.Exec(query, opID, time.Now().UTC(), level, message, details)
	return err
}

type rowScanner interface {
	Scan(dest ...any) error
}

func scanTransferItem(s rowScanner) (*TransferItem, error) {
	item := &TransferItem{}
	var statusStr, vStatusStr string
	var srcHash, remoteHash, errStr sql.NullString
	var transferredAt, verifiedAt, deletedAt sql.NullTime

	err := s.Scan(
		&item.ID,
		&item.OperationID,
		&item.Name,
		&item.IsDirectory,
		&item.RelativePath,
		&item.SourceAbsPath,
		&item.DestinationRelPath,
		&item.SizeBytes,
		&item.MtimeEpoch,
		&item.Inode,
		&item.DeviceID,
		&statusStr,
		&vStatusStr,
		&srcHash,
		&remoteHash,
		&errStr,
		&transferredAt,
		&verifiedAt,
		&deletedAt,
	)
	if err != nil {
		return nil, err
	}

	item.Status = constants.Status(statusStr)
	item.VerificationStatus = constants.VerificationStatus(vStatusStr)
	if srcHash.Valid {
		item.SourceHash = srcHash.String
	}
	if remoteHash.Valid {
		item.RemoteHash = remoteHash.String
	}
	if errStr.Valid {
		item.Error = errStr.String
	}
	if transferredAt.Valid {
		item.TransferredAt = &transferredAt.Time
	}
	if verifiedAt.Valid {
		item.VerifiedAt = &verifiedAt.Time
	}
	if deletedAt.Valid {
		item.DeletedAt = &deletedAt.Time
	}

	return item, nil
}
