package artifacts

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vishu42/openplan/internal/config"
	"github.com/vishu42/openplan/internal/domain"
)

const logContentType = "text/plain; charset=utf-8"

type ObjectStore interface {
	PutObject(ctx context.Context, key string, contentType string, body io.Reader) error
	GetObject(ctx context.Context, key string) ([]byte, error)
	// DeleteObject removes key. Deleting a key that does not exist succeeds.
	DeleteObject(ctx context.Context, key string) error
}

type LogStore struct {
	store ObjectStore
	now   func() time.Time
}

func NewLogStore(store ObjectStore) LogStore {
	return LogStore{store: store, now: time.Now}
}

// PutTemplateRunLog uploads one command's log and returns the metadata describing
// it. It does not record that metadata: the uploader runs on the data plane,
// which has no database, so the workflow hands the returned row to a control
// activity instead. The key is deterministic, so a retried upload overwrites
// rather than duplicates.
func (store LogStore) PutTemplateRunLog(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, name string, body io.Reader) (domain.TemplateRunLog, error) {
	key, err := LogKey(tenantID, runID, name)
	if err != nil {
		return domain.TemplateRunLog{}, err
	}
	content, err := io.ReadAll(body)
	if err != nil {
		return domain.TemplateRunLog{}, fmt.Errorf("read template run log: %w", err)
	}
	if err := store.store.PutObject(ctx, key, logContentType, bytes.NewReader(content)); err != nil {
		return domain.TemplateRunLog{}, fmt.Errorf("put template run log: %w", err)
	}
	now := store.now
	if now == nil {
		now = time.Now
	}
	return domain.TemplateRunLog{
		TenantID:    tenantID,
		RunID:       runID,
		Name:        name,
		ObjectKey:   key,
		ContentType: logContentType,
		SizeBytes:   int64(len(content)),
		UploadedAt:  now().UTC(),
	}, nil
}

func (store LogStore) ReadTemplateRunLog(ctx context.Context, log domain.TemplateRunLog) ([]byte, error) {
	content, err := store.store.GetObject(ctx, log.ObjectKey)
	if err != nil {
		return nil, fmt.Errorf("get template run log: %w", err)
	}
	return content, nil
}

// GetPlanChanges reads a run's uploaded plan changes through the log
// store's object store: the API holds one store for everything it reads.
func (store LogStore) GetPlanChanges(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error) {
	return NewPlanChangesStore(store.store).GetPlanChanges(ctx, tenantID, runID)
}

// DeletePlanChanges deletes a run's uploaded plan changes through the log
// store's object store.
func (store LogStore) DeletePlanChanges(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) error {
	return NewPlanChangesStore(store.store).DeletePlanChanges(ctx, tenantID, runID)
}

// PlanStore keeps a run's saved plan: the encrypted bundle the plan phase
// uploads and the apply phase downloads. It never sees plaintext; encryption
// happens on the executor before Put and after Get.
type PlanStore struct {
	store ObjectStore
}

func NewPlanStore(store ObjectStore) PlanStore {
	return PlanStore{store: store}
}

const planContentType = "application/octet-stream"

func (store PlanStore) PutPlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, sealed []byte) error {
	key, err := PlanKey(tenantID, runID)
	if err != nil {
		return err
	}
	if err := store.store.PutObject(ctx, key, planContentType, bytes.NewReader(sealed)); err != nil {
		return fmt.Errorf("put saved plan: %w", err)
	}
	return nil
}

func (store PlanStore) GetPlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error) {
	key, err := PlanKey(tenantID, runID)
	if err != nil {
		return nil, err
	}
	sealed, err := store.store.GetObject(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get saved plan: %w", err)
	}
	return sealed, nil
}

func (store PlanStore) DeletePlan(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) error {
	key, err := PlanKey(tenantID, runID)
	if err != nil {
		return err
	}
	if err := store.store.DeleteObject(ctx, key); err != nil {
		return fmt.Errorf("delete saved plan: %w", err)
	}
	return nil
}

// PlanKey is where a run's saved plan lives, next to its logs. With the
// filesystem store every executor must share the root: a run's apply can land
// on a different executor from its plan.
func PlanKey(tenantID domain.TenantID, runID domain.TemplateRunID) (string, error) {
	if !safePathComponent(string(tenantID)) || !safePathComponent(string(runID)) {
		return "", fmt.Errorf("tenant ID and run ID must be safe path components")
	}
	return path.Join("tenants", string(tenantID), "runs", string(runID), "plan", "tfplan.bundle.enc"), nil
}

// PlanChangesStore keeps a plan's resource changes, already masked on the
// executor, from the executor's upload until the control plane records them
// in Postgres at FinishPlan.
type PlanChangesStore struct {
	store ObjectStore
}

func NewPlanChangesStore(store ObjectStore) PlanChangesStore {
	return PlanChangesStore{store: store}
}

const planChangesContentType = "application/json"

func (store PlanChangesStore) PutPlanChanges(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID, body []byte) error {
	key, err := PlanChangesKey(tenantID, runID)
	if err != nil {
		return err
	}
	if err := store.store.PutObject(ctx, key, planChangesContentType, bytes.NewReader(body)); err != nil {
		return fmt.Errorf("put plan changes: %w", err)
	}
	return nil
}

// GetPlanChanges returns the run's uploaded changes. A run whose executor
// uploaded none reads as an error wrapping os.ErrNotExist.
func (store PlanChangesStore) GetPlanChanges(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) ([]byte, error) {
	key, err := PlanChangesKey(tenantID, runID)
	if err != nil {
		return nil, err
	}
	body, err := store.store.GetObject(ctx, key)
	if err != nil {
		return nil, fmt.Errorf("get plan changes: %w", err)
	}
	return body, nil
}

// DeletePlanChanges removes the run's uploaded changes once the control plane
// has recorded them. Deleting none succeeds.
func (store PlanChangesStore) DeletePlanChanges(ctx context.Context, tenantID domain.TenantID, runID domain.TemplateRunID) error {
	key, err := PlanChangesKey(tenantID, runID)
	if err != nil {
		return err
	}
	if err := store.store.DeleteObject(ctx, key); err != nil {
		return fmt.Errorf("delete plan changes: %w", err)
	}
	return nil
}

// PlanChangesKey is where a run's plan's resource changes live, beside its
// saved plan. A run has one plan, so the key needs nothing but the run.
func PlanChangesKey(tenantID domain.TenantID, runID domain.TemplateRunID) (string, error) {
	if !safePathComponent(string(tenantID)) || !safePathComponent(string(runID)) {
		return "", fmt.Errorf("tenant ID and run ID must be safe path components")
	}
	return path.Join("tenants", string(tenantID), "runs", string(runID), "plan", "changes.json"), nil
}

func LogKey(tenantID domain.TenantID, runID domain.TemplateRunID, name string) (string, error) {
	if !safePathComponent(string(tenantID)) || !safePathComponent(string(runID)) || !safePathComponent(name) {
		return "", fmt.Errorf("tenant ID, run ID, and log name must be safe path components")
	}
	return path.Join("tenants", string(tenantID), "runs", string(runID), "logs", name+".log"), nil
}

type FilesystemStore struct {
	root string
}

func NewFilesystemStore(root string) FilesystemStore {
	return FilesystemStore{root: root}
}

// The filesystem store is shared by the api, as openplan, and the executor:
// its storage sandboxes as the sandbox user with the group openplan, and the
// worker as root without CAP_DAC_OVERRIDE or CAP_FOWNER (#245). None can
// override another's permissions, so all reach objects through that group:
// directories are setgid, so new files inherit the group, and modes are set
// explicitly, because the umask would strip group write.
const (
	sharedDirMode  = 0o770 | os.ModeSetgid
	sharedFileMode = 0o660
)

func (store FilesystemStore) PutObject(_ context.Context, key string, _ string, body io.Reader) error {
	objectPath, err := store.objectPath(key)
	if err != nil {
		return err
	}
	dir := filepath.Dir(objectPath)
	if err := mkdirShared(store.root, dir); err != nil {
		return fmt.Errorf("create object directory: %w", err)
	}
	// A temporary file of our own, renamed into place: an object already
	// there may belong to the other service, and only its owner can chmod it.
	file, err := os.CreateTemp(dir, ".put-*")
	if err != nil {
		return fmt.Errorf("open object for write: %w", err)
	}
	_, copyErr := io.Copy(file, body)
	chmodErr := file.Chmod(sharedFileMode)
	closeErr := file.Close()
	if err := errors.Join(copyErr, chmodErr, closeErr); err != nil {
		_ = os.Remove(file.Name())
		return fmt.Errorf("write object: %w", err)
	}
	if err := os.Rename(file.Name(), objectPath); err != nil {
		_ = os.Remove(file.Name())
		return fmt.Errorf("store object: %w", err)
	}
	return nil
}

// mkdirShared makes each missing directory from root down to dir, root
// included, shared. A directory that already exists is left alone: it may be
// the other service's. root's parents are nobody's to share.
func mkdirShared(root, dir string) error {
	if err := os.MkdirAll(filepath.Dir(root), 0o755); err != nil { //nolint:gosec // both services walk through to the root
		return err
	}
	if err := mkdirSharedOne(root); err != nil {
		return err
	}
	rel, err := filepath.Rel(root, dir)
	if err != nil || rel == "." {
		return err
	}
	path := root
	for part := range strings.SplitSeq(rel, string(filepath.Separator)) {
		path = filepath.Join(path, part)
		if err := mkdirSharedOne(path); err != nil {
			return err
		}
	}
	return nil
}

// mkdirSharedOne makes path shared, unless it already exists. The mode is set
// explicitly, because the umask would strip group write.
func mkdirSharedOne(path string) error {
	err := os.Mkdir(path, 0o770) //nolint:gosec // the group is how the api and the executor share the store
	if errors.Is(err, fs.ErrExist) {
		return nil
	}
	if err != nil {
		return err
	}
	return os.Chmod(path, sharedDirMode)
}

func (store FilesystemStore) GetObject(_ context.Context, key string) ([]byte, error) {
	objectPath, err := store.objectPath(key)
	if err != nil {
		return nil, err
	}
	content, err := os.ReadFile(objectPath)
	if err != nil {
		return nil, fmt.Errorf("read object: %w", err)
	}
	return content, nil
}

func (store FilesystemStore) DeleteObject(_ context.Context, key string) error {
	objectPath, err := store.objectPath(key)
	if err != nil {
		return err
	}
	if err := os.Remove(objectPath); err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("delete object: %w", err)
	}
	return nil
}

func (store FilesystemStore) objectPath(key string) (string, error) {
	if strings.TrimSpace(store.root) == "" {
		return "", fmt.Errorf("artifact store root is required")
	}
	if !safeObjectKey(key) {
		return "", fmt.Errorf("object key must be a safe object key")
	}
	return filepath.Join(append([]string{store.root}, strings.Split(key, "/")...)...), nil
}

type S3Store struct {
	cfg        config.S3Config
	endpoint   *url.URL
	httpClient *http.Client
	now        func() time.Time
}

func NewS3Store(cfg config.S3Config) (*S3Store, error) {
	if strings.TrimSpace(cfg.Bucket) == "" {
		return nil, fmt.Errorf("s3 bucket is required")
	}
	if strings.TrimSpace(cfg.Region) == "" {
		return nil, fmt.Errorf("s3 region is required")
	}
	if strings.TrimSpace(cfg.Endpoint) == "" {
		cfg.Endpoint = "https://s3." + cfg.Region + ".amazonaws.com"
	}
	if strings.TrimSpace(cfg.AccessKeyID) == "" || strings.TrimSpace(cfg.SecretAccessKey) == "" {
		return nil, fmt.Errorf("s3 access key id and secret access key are required")
	}
	endpoint, err := url.Parse(cfg.Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse s3 endpoint: %w", err)
	}
	if endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("s3 endpoint must include scheme and host")
	}
	return &S3Store{
		cfg:        cfg,
		endpoint:   endpoint,
		httpClient: http.DefaultClient,
		now:        time.Now,
	}, nil
}

func (store *S3Store) PutObject(ctx context.Context, key string, contentType string, body io.Reader) error {
	if !safeObjectKey(key) {
		return fmt.Errorf("object key must be a safe object key")
	}
	content, err := io.ReadAll(body)
	if err != nil {
		return fmt.Errorf("read object body: %w", err)
	}
	request, err := store.newRequest(ctx, http.MethodPut, key, contentType, content)
	if err != nil {
		return err
	}
	response, err := store.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("put s3 object: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return fmt.Errorf("put s3 object: status %d", response.StatusCode)
	}
	return nil
}

func (store *S3Store) GetObject(ctx context.Context, key string) ([]byte, error) {
	if !safeObjectKey(key) {
		return nil, fmt.Errorf("object key must be a safe object key")
	}
	request, err := store.newRequest(ctx, http.MethodGet, key, "", nil)
	if err != nil {
		return nil, err
	}
	response, err := store.httpClient.Do(request)
	if err != nil {
		return nil, fmt.Errorf("get s3 object: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusNotFound {
		return nil, fmt.Errorf("get s3 object: %w", os.ErrNotExist)
	}
	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return nil, fmt.Errorf("get s3 object: status %d", response.StatusCode)
	}
	content, err := io.ReadAll(response.Body)
	if err != nil {
		return nil, fmt.Errorf("read s3 object: %w", err)
	}
	return content, nil
}

// DeleteObject removes key. S3 answers 204 whether or not the key existed.
func (store *S3Store) DeleteObject(ctx context.Context, key string) error {
	if !safeObjectKey(key) {
		return fmt.Errorf("object key must be a safe object key")
	}
	request, err := store.newRequest(ctx, http.MethodDelete, key, "", nil)
	if err != nil {
		return err
	}
	response, err := store.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("delete s3 object: %w", err)
	}
	defer response.Body.Close()
	if (response.StatusCode < 200 || response.StatusCode >= 300) && response.StatusCode != http.StatusNotFound {
		return fmt.Errorf("delete s3 object: status %d", response.StatusCode)
	}
	return nil
}

func (store *S3Store) newRequest(ctx context.Context, method string, key string, contentType string, content []byte) (*http.Request, error) {
	objectURL := store.objectURL(key)
	var body io.Reader
	if content != nil {
		body = bytes.NewReader(content)
	}
	request, err := http.NewRequestWithContext(ctx, method, objectURL.String(), body)
	if err != nil {
		return nil, fmt.Errorf("create s3 request: %w", err)
	}
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}
	store.sign(request, content)
	return request, nil
}

func (store *S3Store) objectURL(key string) url.URL {
	objectURL := *store.endpoint
	escapedKey := escapePath(key)
	if store.cfg.ForcePathStyle {
		objectURL.Path = joinURLPath(objectURL.Path, store.cfg.Bucket, escapedKey)
		return objectURL
	}
	objectURL.Host = store.cfg.Bucket + "." + objectURL.Host
	objectURL.Path = joinURLPath(objectURL.Path, escapedKey)
	return objectURL
}

func (store *S3Store) sign(request *http.Request, content []byte) {
	now := store.now().UTC()
	amzDate := now.Format("20060102T150405Z")
	date := now.Format("20060102")
	payloadHash := sha256Hex(content)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	request.Header.Set("X-Amz-Date", amzDate)
	if request.Header.Get("Host") == "" {
		request.Host = request.URL.Host
	}

	canonicalHeaders, signedHeaders := canonicalHeaders(request)
	canonicalRequest := strings.Join([]string{
		request.Method,
		canonicalURI(request.URL.EscapedPath()),
		canonicalQueryString(request.URL.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	credentialScope := strings.Join([]string{date, store.cfg.Region, "s3", "aws4_request"}, "/")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signingKey := s3SigningKey(store.cfg.SecretAccessKey, date, store.cfg.Region)
	signature := hex.EncodeToString(hmacSHA256(signingKey, stringToSign))
	request.Header.Set("Authorization", fmt.Sprintf(
		"AWS4-HMAC-SHA256 Credential=%s/%s, SignedHeaders=%s, Signature=%s",
		store.cfg.AccessKeyID,
		credentialScope,
		signedHeaders,
		signature,
	))
}

func canonicalHeaders(request *http.Request) (string, string) {
	headers := map[string]string{
		"host":                 request.URL.Host,
		"x-amz-content-sha256": request.Header.Get("X-Amz-Content-Sha256"),
		"x-amz-date":           request.Header.Get("X-Amz-Date"),
	}
	if contentType := request.Header.Get("Content-Type"); contentType != "" {
		headers["content-type"] = contentType
	}
	keys := make([]string, 0, len(headers))
	for key := range headers {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	var builder strings.Builder
	for _, key := range keys {
		builder.WriteString(key)
		builder.WriteByte(':')
		builder.WriteString(strings.TrimSpace(headers[key]))
		builder.WriteByte('\n')
	}
	return builder.String(), strings.Join(keys, ";")
}

func canonicalURI(escapedPath string) string {
	if escapedPath == "" {
		return "/"
	}
	return escapedPath
}

func canonicalQueryString(values url.Values) string {
	if len(values) == 0 {
		return ""
	}
	parts := make([]string, 0, len(values))
	for key, valuesForKey := range values {
		sort.Strings(valuesForKey)
		for _, value := range valuesForKey {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	sort.Strings(parts)
	return strings.Join(parts, "&")
}

func s3SigningKey(secret string, date string, region string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secret), date)
	regionKey := hmacSHA256(dateKey, region)
	serviceKey := hmacSHA256(regionKey, "s3")
	return hmacSHA256(serviceKey, "aws4_request")
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Hex(content []byte) string {
	sum := sha256.Sum256(content)
	return hex.EncodeToString(sum[:])
}

func escapePath(value string) string {
	segments := strings.Split(value, "/")
	for i, segment := range segments {
		segments[i] = url.PathEscape(segment)
	}
	return strings.Join(segments, "/")
}

func joinURLPath(parts ...string) string {
	joined := path.Join(parts...)
	if strings.HasPrefix(parts[0], "/") {
		return "/" + strings.TrimPrefix(joined, "/")
	}
	return "/" + strings.TrimPrefix(joined, "/")
}

func safePathComponent(component string) bool {
	if strings.TrimSpace(component) == "" {
		return false
	}
	if strings.Contains(component, "/") || strings.Contains(component, "\\") {
		return false
	}
	cleaned := path.Clean(component)
	return cleaned == component && component != "." && component != ".."
}

func safeObjectKey(key string) bool {
	if strings.TrimSpace(key) == "" {
		return false
	}
	if strings.HasPrefix(key, "/") || strings.Contains(key, "\\") {
		return false
	}
	cleaned := path.Clean(key)
	if cleaned != key || cleaned == "." || strings.HasPrefix(cleaned, "../") {
		return false
	}
	for segment := range strings.SplitSeq(key, "/") {
		if !safePathComponent(segment) {
			return false
		}
	}
	return true
}
