package workspace

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"crypto/aes"
	"crypto/cipher"
	"crypto/pbkdf2"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	pathpkg "path"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/NorthernReach/contaigen/internal/config"
	"github.com/NorthernReach/contaigen/internal/model"
)

const metadataFile = ".contaigen-workspace.json"

const (
	// Encrypted backups are a Contaigen wrapper around the normal tar.gz
	// payload. The cleartext stream is never written to disk when a password is
	// supplied; writeTarGzip writes directly into encryptedBackupWriter.
	//
	// Format:
	//   magic "C3WSENC1"
	//   16-byte random PBKDF2 salt
	//   4-byte random AES-GCM nonce prefix
	//   uint32 PBKDF2 iteration count
	//   uint32 plaintext chunk size
	//   repeated records: 1-byte type, uint32 ciphertext length, ciphertext
	//
	// Record type is authenticated as AEAD associated data, so data chunks and
	// the final marker cannot be swapped without detection.
	encryptedBackupIterations      = 600_000
	encryptedBackupKeySize         = 32
	encryptedBackupSaltSize        = 16
	encryptedBackupNoncePrefixSize = 4
	encryptedBackupChunkSize       = 64 * 1024
	encryptedBackupRecordData      = byte(0)
	encryptedBackupRecordFinal     = byte(1)
)

var encryptedBackupMagic = []byte("C3WSENC1")

var (
	ErrWorkspaceNotFound = errors.New("workspace not found")
	ErrWorkspaceExists   = errors.New("workspace already exists")
	workspaceNamePattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.-]{0,63}$`)
)

type Manager interface {
	Ensure(context.Context, model.EnsureWorkspaceRequest) (model.Workspace, error)
	Create(context.Context, model.CreateWorkspaceRequest) (model.Workspace, error)
	List(context.Context) ([]model.Workspace, error)
	Inspect(context.Context, string) (model.Workspace, error)
	Backup(context.Context, model.BackupWorkspaceRequest) (model.WorkspaceBackup, error)
	Restore(context.Context, model.RestoreWorkspaceRequest) (model.WorkspaceRestore, error)
	Remove(context.Context, model.RemoveWorkspaceRequest) (model.WorkspaceRemove, error)
}

type Store struct {
	root      string
	backupDir string
	now       func() time.Time
}

type metadata struct {
	Name      string    `json:"name"`
	Path      string    `json:"path"`
	CreatedAt time.Time `json:"createdAt"`
}

func New(paths config.Paths) *Store {
	return &Store{
		root:      paths.WorkspaceDir,
		backupDir: filepath.Join(paths.BackupDir, "workspaces"),
		now:       time.Now,
	}
}

func NewStore(root string, backupDir string) *Store {
	return &Store{
		root:      root,
		backupDir: backupDir,
		now:       time.Now,
	}
}

func (s *Store) Ensure(ctx context.Context, req model.EnsureWorkspaceRequest) (model.Workspace, error) {
	if err := ctx.Err(); err != nil {
		return model.Workspace{}, err
	}
	if req.Name == "" {
		return model.Workspace{}, fmt.Errorf("workspace name is required")
	}

	if req.Path == "" {
		if ws, err := s.Inspect(ctx, req.Name); err == nil {
			return ws, nil
		} else if !errors.Is(err, ErrWorkspaceNotFound) {
			return model.Workspace{}, err
		}
	}

	return s.Create(ctx, model.CreateWorkspaceRequest{
		Name: req.Name,
		Path: req.Path,
	})
}

func (s *Store) Create(ctx context.Context, req model.CreateWorkspaceRequest) (model.Workspace, error) {
	if err := ctx.Err(); err != nil {
		return model.Workspace{}, err
	}
	if err := validateName(req.Name); err != nil {
		return model.Workspace{}, err
	}

	path, err := s.workspacePath(req.Name, req.Path)
	if err != nil {
		return model.Workspace{}, err
	}
	if req.Path == "" {
		if _, err := os.Stat(path); err == nil {
			return model.Workspace{}, fmt.Errorf("%w: %s", ErrWorkspaceExists, req.Name)
		} else if !os.IsNotExist(err) {
			return model.Workspace{}, err
		}
	}

	if err := os.MkdirAll(path, 0o750); err != nil {
		return model.Workspace{}, fmt.Errorf("create workspace directory: %w", err)
	}

	ws := model.Workspace{
		Name:      req.Name,
		Path:      path,
		CreatedAt: s.now().UTC(),
	}
	if err := writeMetadata(ws); err != nil {
		return model.Workspace{}, err
	}
	return ws, nil
}

func (s *Store) List(ctx context.Context) ([]model.Workspace, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	entries, err := os.ReadDir(s.root)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	workspaces := make([]model.Workspace, 0, len(entries))
	for _, entry := range entries {
		if !entry.IsDir() || strings.HasPrefix(entry.Name(), ".") {
			continue
		}
		ws, err := s.Inspect(ctx, entry.Name())
		if err != nil {
			continue
		}
		workspaces = append(workspaces, ws)
	}
	sort.Slice(workspaces, func(i, j int) bool {
		return workspaces[i].Name < workspaces[j].Name
	})
	return workspaces, nil
}

func (s *Store) Inspect(ctx context.Context, name string) (model.Workspace, error) {
	if err := ctx.Err(); err != nil {
		return model.Workspace{}, err
	}
	return s.inspectWorkspacePath(name, "")
}

func (s *Store) Backup(ctx context.Context, req model.BackupWorkspaceRequest) (model.WorkspaceBackup, error) {
	if err := ctx.Err(); err != nil {
		return model.WorkspaceBackup{}, err
	}

	ws, err := s.inspectWorkspacePath(req.Name, req.Path)
	if err != nil {
		return model.WorkspaceBackup{}, err
	}

	outputPath := req.OutputPath
	if outputPath == "" {
		extension := ".tar.gz"
		if req.Password != "" {
			extension = ".tar.gz.c3enc"
		}
		outputPath = filepath.Join(s.backupDir, fmt.Sprintf("%s-%s%s", ws.Name, s.now().UTC().Format("20060102T150405Z"), extension))
	}
	outputPath, err = filepath.Abs(outputPath)
	if err != nil {
		return model.WorkspaceBackup{}, err
	}
	if err := os.MkdirAll(filepath.Dir(outputPath), 0o750); err != nil {
		return model.WorkspaceBackup{}, fmt.Errorf("create backup directory: %w", err)
	}

	size, err := writeWorkspaceBackup(ws, outputPath, req.Password)
	if err != nil {
		return model.WorkspaceBackup{}, err
	}

	return model.WorkspaceBackup{
		Workspace: ws,
		Path:      outputPath,
		SizeBytes: size,
		Encrypted: req.Password != "",
	}, nil
}

func (s *Store) Restore(ctx context.Context, req model.RestoreWorkspaceRequest) (model.WorkspaceRestore, error) {
	if err := ctx.Err(); err != nil {
		return model.WorkspaceRestore{}, err
	}
	if err := validateName(req.Name); err != nil {
		return model.WorkspaceRestore{}, err
	}
	if strings.TrimSpace(req.InputPath) == "" {
		return model.WorkspaceRestore{}, fmt.Errorf("workspace backup path is required")
	}

	inputPath, err := filepath.Abs(req.InputPath)
	if err != nil {
		return model.WorkspaceRestore{}, err
	}
	if _, err := os.Stat(inputPath); err != nil {
		return model.WorkspaceRestore{}, fmt.Errorf("workspace backup %q: %w", inputPath, err)
	}

	targetPath, err := s.workspacePath(req.Name, req.Path)
	if err != nil {
		return model.WorkspaceRestore{}, err
	}
	if err := ensureRestoreTarget(targetPath); err != nil {
		return model.WorkspaceRestore{}, err
	}
	if err := os.MkdirAll(targetPath, 0o750); err != nil {
		return model.WorkspaceRestore{}, fmt.Errorf("create workspace directory: %w", err)
	}

	files, size, err := extractWorkspaceBackup(inputPath, targetPath, req.Password)
	if err != nil {
		return model.WorkspaceRestore{}, err
	}
	ws := model.Workspace{
		Name:      req.Name,
		Path:      targetPath,
		CreatedAt: s.now().UTC(),
	}
	if err := writeMetadata(ws); err != nil {
		return model.WorkspaceRestore{}, err
	}

	return model.WorkspaceRestore{
		Workspace: ws,
		Path:      inputPath,
		Files:     files,
		SizeBytes: size,
	}, nil
}

func (s *Store) Remove(ctx context.Context, req model.RemoveWorkspaceRequest) (model.WorkspaceRemove, error) {
	if err := ctx.Err(); err != nil {
		return model.WorkspaceRemove{}, err
	}
	ws, err := s.inspectWorkspacePath(req.Name, req.Path)
	if err != nil {
		return model.WorkspaceRemove{}, err
	}

	if err := os.RemoveAll(ws.Path); err != nil {
		return model.WorkspaceRemove{}, fmt.Errorf("remove workspace directory: %w", err)
	}
	return model.WorkspaceRemove{Workspace: ws}, nil
}

func (s *Store) inspectWorkspacePath(name string, customPath string) (model.Workspace, error) {
	if err := validateName(name); err != nil {
		return model.Workspace{}, err
	}

	path, err := s.workspacePath(name, customPath)
	if err != nil {
		return model.Workspace{}, err
	}
	info, err := os.Stat(path)
	if err != nil {
		if os.IsNotExist(err) {
			return model.Workspace{}, fmt.Errorf("%w: %s", ErrWorkspaceNotFound, name)
		}
		return model.Workspace{}, err
	}
	if !info.IsDir() {
		return model.Workspace{}, fmt.Errorf("workspace path is not a directory: %s", path)
	}

	ws, err := readMetadata(path)
	if err == nil {
		if ws.Name != name {
			return model.Workspace{}, fmt.Errorf("workspace metadata name %q does not match requested workspace %q", ws.Name, name)
		}
		return ws, nil
	}
	if customPath != "" && !samePath(path, filepath.Join(s.root, name)) {
		return model.Workspace{}, fmt.Errorf("refusing to use custom workspace path without Contaigen metadata: %s", path)
	}

	return model.Workspace{
		Name:      name,
		Path:      path,
		CreatedAt: info.ModTime(),
	}, nil
}

func (s *Store) workspacePath(name string, customPath string) (string, error) {
	if customPath != "" {
		return filepath.Abs(customPath)
	}
	return filepath.Abs(filepath.Join(s.root, name))
}

func samePath(left string, right string) bool {
	leftAbs, leftErr := filepath.Abs(left)
	rightAbs, rightErr := filepath.Abs(right)
	if leftErr != nil || rightErr != nil {
		return false
	}
	return filepath.Clean(leftAbs) == filepath.Clean(rightAbs)
}

func ensureRestoreTarget(targetPath string) error {
	info, err := os.Stat(targetPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	if !info.IsDir() {
		return fmt.Errorf("workspace restore target exists and is not a directory: %s", targetPath)
	}
	entries, err := os.ReadDir(targetPath)
	if err != nil {
		return err
	}
	if len(entries) > 0 {
		return fmt.Errorf("workspace restore target already exists and is not empty: %s", targetPath)
	}
	return nil
}

func validateName(name string) error {
	if name == "" {
		return fmt.Errorf("workspace name is required")
	}
	if !workspaceNamePattern.MatchString(name) {
		return fmt.Errorf("workspace name %q must start with a letter or number and contain only letters, numbers, dots, underscores, or dashes", name)
	}
	return nil
}

func writeMetadata(ws model.Workspace) error {
	data, err := json.MarshalIndent(metadata{
		Name:      ws.Name,
		Path:      ws.Path,
		CreatedAt: ws.CreatedAt,
	}, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(filepath.Join(ws.Path, metadataFile), data, 0o600)
}

func readMetadata(path string) (model.Workspace, error) {
	data, err := os.ReadFile(filepath.Join(path, metadataFile))
	if err != nil {
		return model.Workspace{}, err
	}

	var meta metadata
	if err := json.Unmarshal(data, &meta); err != nil {
		return model.Workspace{}, err
	}
	if meta.Path == "" {
		meta.Path = path
	}
	return model.Workspace{
		Name:      meta.Name,
		Path:      meta.Path,
		CreatedAt: meta.CreatedAt,
	}, nil
}

func writeWorkspaceBackup(ws model.Workspace, outputPath string, password string) (int64, error) {
	file, err := os.Create(outputPath)
	if err != nil {
		return 0, err
	}
	closed := false
	defer func() {
		if !closed {
			_ = file.Close()
		}
	}()

	writer := io.Writer(file)
	var encrypted *encryptedBackupWriter
	if password != "" {
		// Encryption wraps the output file as an io.Writer. The tar.gz writer
		// below does not need to know whether the final archive is encrypted.
		encrypted, err = newEncryptedBackupWriter(file, password)
		if err != nil {
			return 0, err
		}
		writer = encrypted
	}

	if err := writeTarGzip(ws, writer); err != nil {
		if encrypted != nil {
			_ = encrypted.Close()
		}
		return 0, err
	}
	if encrypted != nil {
		if err := encrypted.Close(); err != nil {
			return 0, err
		}
	}
	if err := file.Close(); err != nil {
		return 0, err
	}
	closed = true

	info, err := os.Stat(outputPath)
	if err != nil {
		return 0, err
	}
	return info.Size(), nil
}

func writeTarGzip(ws model.Workspace, writer io.Writer) error {
	gzipWriter := gzip.NewWriter(writer)
	tarWriter := tar.NewWriter(gzipWriter)

	if err := filepath.WalkDir(ws.Path, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}

		info, err := entry.Info()
		if err != nil {
			return err
		}

		rel, err := filepath.Rel(ws.Path, path)
		if err != nil {
			return err
		}
		if rel == "." {
			rel = ""
		}

		link := ""
		if info.Mode()&os.ModeSymlink != 0 {
			link, err = os.Readlink(path)
			if err != nil {
				return err
			}
		}

		header, err := tar.FileInfoHeader(info, link)
		if err != nil {
			return err
		}
		header.Name = filepath.ToSlash(filepath.Join(ws.Name, rel))
		if header.Name == "." {
			header.Name = ws.Name
		}

		if err := tarWriter.WriteHeader(header); err != nil {
			return err
		}
		if entry.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return nil
		}

		input, err := os.Open(path)
		if err != nil {
			return err
		}

		_, err = io.Copy(tarWriter, input)
		if closeErr := input.Close(); err == nil {
			err = closeErr
		}
		return err
	}); err != nil {
		return err
	}

	if err := tarWriter.Close(); err != nil {
		return err
	}
	if err := gzipWriter.Close(); err != nil {
		return err
	}
	return nil
}

func extractWorkspaceBackup(inputPath string, targetPath string, password string) (int, int64, error) {
	file, err := os.Open(inputPath)
	if err != nil {
		return 0, 0, err
	}
	defer file.Close()

	// Peek at the magic header to decide whether this is a plain tar.gz backup
	// or Contaigen's encrypted wrapper, then rewind before building the reader.
	header := make([]byte, len(encryptedBackupMagic))
	n, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return 0, 0, err
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return 0, 0, err
	}

	reader := io.Reader(file)
	if n == len(encryptedBackupMagic) && bytes.Equal(header, encryptedBackupMagic) {
		if password == "" {
			return 0, 0, fmt.Errorf("encrypted workspace backup requires a password")
		}
		reader, err = newEncryptedBackupReader(file, password)
		if err != nil {
			return 0, 0, err
		}
	} else if password != "" {
		return 0, 0, fmt.Errorf("password was supplied, but workspace backup is not encrypted")
	}

	return extractTarGzip(reader, targetPath)
}

func extractTarGzip(reader io.Reader, targetPath string) (int, int64, error) {
	gzipReader, err := gzip.NewReader(reader)
	if err != nil {
		return 0, 0, err
	}
	defer gzipReader.Close()

	tarReader := tar.NewReader(gzipReader)
	files := 0
	var size int64
	for {
		header, err := tarReader.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return 0, 0, err
		}

		rel, skip, err := restoreRelativePath(header.Name)
		if err != nil {
			return 0, 0, err
		}
		if skip || rel == metadataFile {
			continue
		}

		target, err := safeRestorePath(targetPath, rel)
		if err != nil {
			return 0, 0, err
		}
		mode := header.FileInfo().Mode().Perm()
		if mode == 0 {
			mode = 0o640
		}

		switch header.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, mode); err != nil {
				return 0, 0, err
			}
		case tar.TypeReg, tar.TypeRegA:
			if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
				return 0, 0, err
			}
			written, err := restoreRegularFile(tarReader, target, mode)
			if err != nil {
				return 0, 0, err
			}
			files++
			size += written
		case tar.TypeSymlink:
			return 0, 0, fmt.Errorf("unsupported symbolic link in workspace archive: %s", header.Name)
		default:
			return 0, 0, fmt.Errorf("unsupported archive entry %q type %d", header.Name, header.Typeflag)
		}
	}
	return files, size, nil
}

func restoreRegularFile(reader io.Reader, target string, mode os.FileMode) (int64, error) {
	file, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, mode)
	if err != nil {
		return 0, err
	}
	written, copyErr := io.Copy(file, reader)
	closeErr := file.Close()
	if copyErr != nil {
		return written, copyErr
	}
	return written, closeErr
}

func restoreRelativePath(name string) (string, bool, error) {
	name = strings.TrimSpace(strings.ReplaceAll(name, "\\", "/"))
	name = strings.TrimPrefix(name, "./")
	if name == "" || strings.HasPrefix(name, "/") {
		return "", false, fmt.Errorf("unsafe archive path %q", name)
	}
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return "", false, fmt.Errorf("unsafe archive path %q", name)
		}
	}
	clean := pathpkg.Clean(name)
	if clean == "." {
		return "", true, nil
	}
	parts := strings.Split(clean, "/")
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false, fmt.Errorf("unsafe archive path %q", name)
		}
	}
	if len(parts) == 1 {
		return "", true, nil
	}
	return pathpkg.Join(parts[1:]...), false, nil
}

func safeRestorePath(root string, rel string) (string, error) {
	target := filepath.Join(root, filepath.FromSlash(rel))
	cleanRoot, err := filepath.Abs(root)
	if err != nil {
		return "", err
	}
	cleanTarget, err := filepath.Abs(target)
	if err != nil {
		return "", err
	}
	relToRoot, err := filepath.Rel(cleanRoot, cleanTarget)
	if err != nil {
		return "", err
	}
	if relToRoot == ".." || strings.HasPrefix(relToRoot, ".."+string(filepath.Separator)) || filepath.IsAbs(relToRoot) {
		return "", fmt.Errorf("archive path escapes workspace: %s", rel)
	}
	return cleanTarget, nil
}

type encryptedBackupWriter struct {
	writer      io.Writer
	aead        cipher.AEAD
	noncePrefix []byte
	counter     uint64
	buffer      []byte
	closed      bool
}

func newEncryptedBackupWriter(writer io.Writer, password string) (*encryptedBackupWriter, error) {
	if password == "" {
		return nil, fmt.Errorf("encryption password is required")
	}
	salt := make([]byte, encryptedBackupSaltSize)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}
	noncePrefix := make([]byte, encryptedBackupNoncePrefixSize)
	if _, err := rand.Read(noncePrefix); err != nil {
		return nil, err
	}
	// Each archive gets a fresh salt and nonce prefix. Chunk counters complete
	// the nonce, giving every AES-GCM record a unique nonce for the derived key.
	aead, err := encryptedBackupAEAD(password, salt, encryptedBackupIterations)
	if err != nil {
		return nil, err
	}

	header := make([]byte, 0, len(encryptedBackupMagic)+encryptedBackupSaltSize+encryptedBackupNoncePrefixSize+8)
	header = append(header, encryptedBackupMagic...)
	header = append(header, salt...)
	header = append(header, noncePrefix...)
	header = binary.BigEndian.AppendUint32(header, encryptedBackupIterations)
	header = binary.BigEndian.AppendUint32(header, encryptedBackupChunkSize)
	if _, err := writer.Write(header); err != nil {
		return nil, err
	}
	return &encryptedBackupWriter{
		writer:      writer,
		aead:        aead,
		noncePrefix: noncePrefix,
		buffer:      make([]byte, 0, encryptedBackupChunkSize),
	}, nil
}

func (w *encryptedBackupWriter) Write(data []byte) (int, error) {
	if w.closed {
		return 0, fmt.Errorf("encrypted backup writer is closed")
	}
	written := 0
	for len(data) > 0 {
		// Buffer plaintext into fixed-size records so large backups can stream
		// without holding the full tar.gz payload in memory.
		space := encryptedBackupChunkSize - len(w.buffer)
		if space > len(data) {
			space = len(data)
		}
		w.buffer = append(w.buffer, data[:space]...)
		data = data[space:]
		written += space
		if len(w.buffer) == encryptedBackupChunkSize {
			if err := w.flush(encryptedBackupRecordData, w.buffer); err != nil {
				return written, err
			}
			w.buffer = w.buffer[:0]
		}
	}
	return written, nil
}

func (w *encryptedBackupWriter) Close() error {
	if w.closed {
		return nil
	}
	if len(w.buffer) > 0 {
		if err := w.flush(encryptedBackupRecordData, w.buffer); err != nil {
			return err
		}
		w.buffer = w.buffer[:0]
	}
	if err := w.flush(encryptedBackupRecordFinal, nil); err != nil {
		return err
	}
	w.closed = true
	return nil
}

func (w *encryptedBackupWriter) flush(recordType byte, plaintext []byte) error {
	nonce, err := encryptedBackupNonce(w.noncePrefix, w.counter)
	if err != nil {
		return err
	}
	// Bind the record type into the authentication tag. A final record cannot
	// be replayed as a data record, and vice versa.
	ciphertext := w.aead.Seal(nil, nonce, plaintext, []byte{recordType})
	recordHeader := []byte{recordType, 0, 0, 0, 0}
	binary.BigEndian.PutUint32(recordHeader[1:], uint32(len(ciphertext)))
	if _, err := w.writer.Write(recordHeader); err != nil {
		return err
	}
	if _, err := w.writer.Write(ciphertext); err != nil {
		return err
	}
	w.counter++
	return nil
}

type encryptedBackupReader struct {
	reader      io.Reader
	aead        cipher.AEAD
	noncePrefix []byte
	chunkSize   uint32
	counter     uint64
	buffer      []byte
	eof         bool
}

func newEncryptedBackupReader(reader io.Reader, password string) (*encryptedBackupReader, error) {
	if password == "" {
		return nil, fmt.Errorf("encrypted workspace backup requires a password")
	}
	magic := make([]byte, len(encryptedBackupMagic))
	if _, err := io.ReadFull(reader, magic); err != nil {
		return nil, err
	}
	if !bytes.Equal(magic, encryptedBackupMagic) {
		return nil, fmt.Errorf("workspace backup is not encrypted")
	}
	// The salt, nonce prefix, and KDF parameters are stored in cleartext because
	// they are not secret; they are required to derive the same key for restore.
	salt := make([]byte, encryptedBackupSaltSize)
	if _, err := io.ReadFull(reader, salt); err != nil {
		return nil, err
	}
	noncePrefix := make([]byte, encryptedBackupNoncePrefixSize)
	if _, err := io.ReadFull(reader, noncePrefix); err != nil {
		return nil, err
	}
	var params [8]byte
	if _, err := io.ReadFull(reader, params[:]); err != nil {
		return nil, err
	}
	iterations := int(binary.BigEndian.Uint32(params[0:4]))
	chunkSize := binary.BigEndian.Uint32(params[4:8])
	if iterations <= 0 || chunkSize == 0 || chunkSize > encryptedBackupChunkSize {
		return nil, fmt.Errorf("encrypted workspace backup has unsupported parameters")
	}
	aead, err := encryptedBackupAEAD(password, salt, iterations)
	if err != nil {
		return nil, err
	}
	return &encryptedBackupReader{
		reader:      reader,
		aead:        aead,
		noncePrefix: noncePrefix,
		chunkSize:   chunkSize,
	}, nil
}

func (r *encryptedBackupReader) Read(data []byte) (int, error) {
	for len(r.buffer) == 0 && !r.eof {
		if err := r.readRecord(); err != nil {
			return 0, err
		}
	}
	if len(r.buffer) == 0 && r.eof {
		return 0, io.EOF
	}
	n := copy(data, r.buffer)
	r.buffer = r.buffer[n:]
	return n, nil
}

func (r *encryptedBackupReader) readRecord() error {
	var recordHeader [5]byte
	if _, err := io.ReadFull(r.reader, recordHeader[:]); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return fmt.Errorf("encrypted workspace backup ended before final record")
		}
		return err
	}
	recordType := recordHeader[0]
	ciphertextLen := binary.BigEndian.Uint32(recordHeader[1:])
	if recordType != encryptedBackupRecordData && recordType != encryptedBackupRecordFinal {
		return fmt.Errorf("encrypted workspace backup has unsupported record type %d", recordType)
	}
	maxCiphertextLen := r.chunkSize + uint32(r.aead.Overhead())
	if ciphertextLen > maxCiphertextLen {
		return fmt.Errorf("encrypted workspace backup record is too large")
	}
	ciphertext := make([]byte, ciphertextLen)
	if _, err := io.ReadFull(r.reader, ciphertext); err != nil {
		return err
	}
	nonce, err := encryptedBackupNonce(r.noncePrefix, r.counter)
	if err != nil {
		return err
	}
	// Authentication failures here usually mean the wrong password, a corrupted
	// archive, or tampering with the encrypted backup file.
	plaintext, err := r.aead.Open(nil, nonce, ciphertext, []byte{recordType})
	if err != nil {
		return fmt.Errorf("decrypt encrypted workspace backup: %w", err)
	}
	r.counter++
	if recordType == encryptedBackupRecordFinal {
		if len(plaintext) != 0 {
			return fmt.Errorf("encrypted workspace backup final record is invalid")
		}
		r.eof = true
		return nil
	}
	r.buffer = plaintext
	return nil
}

func encryptedBackupAEAD(password string, salt []byte, iterations int) (cipher.AEAD, error) {
	// PBKDF2 turns a human password into the AES-256 key. This keeps backups
	// portable without requiring a host keychain or Contaigen-managed secret.
	key, err := pbkdf2.Key(sha256.New, password, salt, iterations, encryptedBackupKeySize)
	if err != nil {
		return nil, err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	return cipher.NewGCM(block)
}

func encryptedBackupNonce(prefix []byte, counter uint64) ([]byte, error) {
	if len(prefix) != encryptedBackupNoncePrefixSize {
		return nil, fmt.Errorf("invalid encrypted backup nonce prefix")
	}
	nonce := make([]byte, encryptedBackupNoncePrefixSize+8)
	copy(nonce, prefix)
	// AES-GCM requires nonce uniqueness for a given key. The random per-archive
	// prefix plus monotonically increasing chunk counter gives that property.
	binary.BigEndian.PutUint64(nonce[encryptedBackupNoncePrefixSize:], counter)
	return nonce, nil
}
