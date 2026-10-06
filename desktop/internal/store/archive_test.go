package store

import (
	"bytes"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const rustPassphrase = "rust-passphrase-🔒"

// These fixed fixtures were encrypted by Rust aes-gcm 0.10 / scrypt 0.11,
// using the legacy command's framing and parameters. Salt=00..0f, nonce=10..1b.
func TestRustArchiveFixtures(t *testing.T) {
	hashes := map[string]string{"1": "35e360c1400c702b34740616171cbcb2e7d3124d53329d645c374b8d1382d316", "2": "cfb01a929a863f1e06a386b1e3250d2a70e92ef661d2703ab7630e61248d8df4"}
	for version, wantHash := range hashes {
		t.Run("v"+version, func(t *testing.T) {
			path := filepath.Join("testdata", "legacy-rust-v"+version+".mzar")
			b, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			sum := sha256.Sum256(b)
			if hex.EncodeToString(sum[:]) != wantHash {
				t.Fatal("Rust fixture changed")
			}
			plain, err := decryptArchive(b, rustPassphrase)
			if err != nil {
				t.Fatal(err)
			}
			aead, err := archiveCipher(rustPassphrase, b[6:22])
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.Equal(aead.Seal(nil, b[22:34], plain, nil), b[34:]) {
				t.Fatal("Go ciphertext differs from Rust")
			}
			s := openTestStore(t)
			id, err := s.ImportArchive(rustPassphrase, path)
			if err != nil {
				t.Fatal(err)
			}
			if id != "rust-fixture" {
				t.Fatal(id)
			}
			p, err := s.ProfileGet(id)
			if err != nil {
				t.Fatal(err)
			}
			if p["lastOpenedAt"] != "2025-01-03T00:00:00+00:00" || p["proxyCountry"] != "US" {
				t.Fatal("import lost persisted metadata")
			}
			quota := p["fingerprint"].(map[string]any)["storageQuota"]
			if quota != json.Number("18446744073709551615") {
				t.Fatalf("quota lost precision: %v", quota)
			}
			cookies, err := os.ReadFile(filepath.Join(text(p["dataDir"]), "Default", "Cookies"))
			if err != nil || string(cookies) != "abc" {
				t.Fatal(string(cookies), err)
			}
			if version == "2" {
				options := p["chromixOptions"].(map[string]any)
				if options["max"] != json.Number("18446744073709551615") || options["false"] != false {
					t.Fatal(options)
				}
				ext := p["extensions"].([]any)[0].(map[string]any)
				wantDir := filepath.Join(s.DataDir, "extensions", "shared-ext", "1.0")
				if ext["dir"] != wantDir {
					t.Fatal(ext)
				}
				data, err := os.ReadFile(filepath.Join(wantDir, "manifest.json"))
				if err != nil || string(data) != "{}" {
					t.Fatal(string(data), err)
				}
			} else {
				jsonEqual(t, map[string]any{}, p["chromixOptions"])
			}
			again, err := s.ImportArchive(rustPassphrase, path)
			if err != nil {
				t.Fatal(err)
			}
			if again == id || !safeSegment(again) {
				t.Fatal("collision not remapped", again)
			}
		})
	}
}
func writeTestArchive(t *testing.T, path string, m archiveManifest, contents [][]byte) {
	t.Helper()
	b, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	var plain bytes.Buffer
	if err = putChunk(&plain, b); err != nil {
		t.Fatal(err)
	}
	for _, data := range contents {
		if err = putChunk(&plain, data); err != nil {
			t.Fatal(err)
		}
	}
	encrypted, err := encryptArchive(plain.Bytes(), "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	if err = os.WriteFile(path, encrypted, 0600); err != nil {
		t.Fatal(err)
	}
}
func testMeta(path string, b []byte) archiveFile {
	sum := sha256.Sum256(b)
	return archiveFile{Path: path, Size: uint64(len(b)), SHA256: hex.EncodeToString(sum[:])}
}
func TestArchiveRoundTripExtensionsAndPrecision(t *testing.T) {
	source := openTestStore(t)
	p := createTestProfile(t, source)
	id := text(p["id"])
	dir := text(p["dataDir"])
	shared := filepath.Join(source.DataDir, "extensions", "shared", "1.2")
	local := filepath.Join(dir, "extensions", "local")
	for _, dir := range []string{shared, local} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "manifest.json"), []byte(`{"version":"1.2"}`), 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "Cookies"), []byte{0, 1, 2, 3, 255}, 0600); err != nil {
		t.Fatal(err)
	}
	extensions := []any{map[string]any{"id": "shared", "name": "Shared", "version": "1.2", "enabled": true, "scope": "shared", "dir": shared, "source": "file"}, map[string]any{"id": "local", "name": "Local", "version": "1.2", "enabled": false, "scope": "profile", "dir": local, "source": "folder"}}
	p, err := source.ProfileUpdate(id, map[string]any{"extensions": extensions, "chromixOptions": mustJSON(t, arbitraryOptions)})
	if err != nil {
		t.Fatal(err)
	}
	if err = source.MarkOpened(id); err != nil {
		t.Fatal(err)
	}
	if err = source.SetProxyCountry(id, "GB"); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "export.mzar")
	if err = source.ExportArchive(id, "passphrase", path); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if string(raw[:4]) != "MZAR" || binary.BigEndian.Uint16(raw[4:6]) != 2 {
		t.Fatal("bad header")
	}
	plain, err := decryptArchive(raw, "passphrase")
	if err != nil {
		t.Fatal(err)
	}
	manifestBytes, err := readChunk(&plain)
	if err != nil {
		t.Fatal(err)
	}
	var manifest archiveManifest
	if err = decodeJSON(manifestBytes, &manifest); err != nil {
		t.Fatal(err)
	}
	if len(manifest.Extensions) != 1 || len(manifest.Files) != 2 {
		t.Fatal(manifest)
	}
	target := openTestStore(t)
	restored, err := target.ImportArchive("passphrase", path)
	if err != nil {
		t.Fatal(err)
	}
	if restored != id {
		t.Fatal("unused id not preserved")
	}
	got, err := target.ProfileGet(restored)
	if err != nil {
		t.Fatal(err)
	}
	jsonEqual(t, p["chromixOptions"], got["chromixOptions"])
	jsonEqual(t, p["fingerprint"], got["fingerprint"])
	if got["proxyCountry"] != "GB" || got["lastOpenedAt"] == nil {
		t.Fatal(got)
	}
	for _, v := range got["extensions"].([]any) {
		ext := v.(map[string]any)
		if !strings.HasPrefix(text(ext["dir"]), target.DataDir+string(filepath.Separator)) {
			t.Fatal("extension path not relocated")
		}
		b, err := os.ReadFile(filepath.Join(text(ext["dir"]), "manifest.json"))
		if err != nil || string(b) != `{"version":"1.2"}` {
			t.Fatal(string(b), err)
		}
	}
	b, err := os.ReadFile(filepath.Join(text(got["dataDir"]), "Cookies"))
	if err != nil || !bytes.Equal(b, []byte{0, 1, 2, 3, 255}) {
		t.Fatal(b, err)
	}
	if err = source.ExportArchive(id, "short", path); err == nil {
		t.Fatal("short passphrase accepted")
	}
	if err = source.ExportArchive("missing", "passphrase", path); err == nil {
		t.Fatal("missing profile exported")
	}
	if _, err = target.ImportArchive("wrong passphrase", path); err == nil {
		t.Fatal("wrong passphrase accepted")
	}
	raw[len(raw)-1] ^= 1
	if err = os.WriteFile(path, raw, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err = target.ImportArchive("passphrase", path); err == nil {
		t.Fatal("tampered archive accepted")
	}
}
func TestArchiveRejectsUnsafePathsWithoutSideEffects(t *testing.T) {
	for _, path := range []string{"../escape", "../sibling/escape", "/absolute", "C:/Windows/escape", `..\escape`, "a/../../escape", "a//b", ".", "a/./b", "a\x00b"} {
		t.Run(path, func(t *testing.T) {
			s := openTestStore(t)
			p := createTestProfile(t, s)
			id := text(p["id"])
			if err := s.ProfileDelete(id); err != nil {
				t.Fatal(err)
			}
			m := archiveManifest{Magic: "MZAR", Version: 2, Profile: p, Files: []archiveFile{testMeta(path, []byte("data"))}}
			archive := filepath.Join(t.TempDir(), "unsafe.mzar")
			writeTestArchive(t, archive, m, [][]byte{[]byte("data")})
			if _, err := s.ImportArchive("passphrase", archive); err == nil {
				t.Fatal("unsafe path accepted")
			}
			profiles, err := s.ProfilesList()
			if err != nil || len(profiles) != 0 {
				t.Fatal("partial profile inserted", profiles, err)
			}
			entries, err := os.ReadDir(filepath.Join(s.DataDir, "profiles"))
			if err != nil || len(entries) != 0 {
				t.Fatal("partial directory written", err)
			}
		})
	}
}
func TestArchiveChecksumsLengthsAndCollisions(t *testing.T) {
	s := openTestStore(t)
	p := createTestProfile(t, s)
	if err := s.ProfileDelete(text(p["id"])); err != nil {
		t.Fatal(err)
	}
	base := archiveManifest{Magic: "MZAR", Version: 2, Profile: p, Files: []archiveFile{testMeta("file", []byte("data"))}}
	for _, kind := range []string{"checksum", "size", "duplicate", "truncated"} {
		t.Run(kind, func(t *testing.T) {
			m := base
			m.Files = append([]archiveFile{}, base.Files...)
			contents := [][]byte{[]byte("data")}
			switch kind {
			case "checksum":
				m.Files[0].SHA256 = strings.Repeat("0", 64)
			case "size":
				m.Files[0].Size = 5
			case "duplicate":
				m.Files = append(m.Files, m.Files[0])
				contents = append(contents, []byte("data"))
			case "truncated":
				contents = nil
			}
			path := filepath.Join(t.TempDir(), "bad.mzar")
			writeTestArchive(t, path, m, contents)
			if _, err := s.ImportArchive("passphrase", path); err == nil {
				t.Fatal("invalid archive accepted")
			}
		})
	}
	existingDir := text(p["dataDir"])
	if err := os.Mkdir(existingDir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(existingDir, "keep"), []byte("keep"), 0600); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "good.mzar")
	writeTestArchive(t, path, base, [][]byte{[]byte("data")})
	id, err := s.ImportArchive("passphrase", path)
	if err != nil {
		t.Fatal(err)
	}
	if id == p["id"] {
		t.Fatal("untracked directory overwritten")
	}
	if _, err = os.Stat(filepath.Join(existingDir, "keep")); err != nil {
		t.Fatal(err)
	}
}
func TestArchiveSymlinkGuards(t *testing.T) {
	s := openTestStore(t)
	p := createTestProfile(t, s)
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(outside, "secret"), []byte("secret"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(text(p["dataDir"]), "link")
	if err := os.Symlink(outside, link); err != nil {
		t.Skip(err)
	}
	if err := s.ExportArchive(text(p["id"]), "passphrase", filepath.Join(t.TempDir(), "out.mzar")); err == nil {
		t.Fatal("export followed symlink")
	}
	extRoot := filepath.Join(s.DataDir, "extensions", "shared-ext")
	if err := os.Symlink(outside, extRoot); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportArchive(rustPassphrase, filepath.Join("testdata", "legacy-rust-v2.mzar")); err == nil {
		t.Fatal("import followed extension symlink")
	}
	if p, err := s.ProfileGet("rust-fixture"); err != nil || p != nil {
		t.Fatal("failed import left profile", err)
	}
	if _, err := os.Stat(filepath.Join(outside, "1.0")); !os.IsNotExist(err) {
		t.Fatal("wrote through symlink")
	}
}
func TestArchiveExistingExtensionIsNotOverwritten(t *testing.T) {
	s := openTestStore(t)
	dir := filepath.Join(s.DataDir, "extensions", "shared-ext", "1.0")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(file, []byte("keep existing"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.ImportArchive(rustPassphrase, filepath.Join("testdata", "legacy-rust-v2.mzar")); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(file)
	if err != nil || string(b) != "keep existing" {
		t.Fatal(string(b), err)
	}
}
func TestArchiveHeaderValidation(t *testing.T) {
	for _, buf := range [][]byte{nil, []byte("MZAR"), make([]byte, 50), append([]byte{'M', 'Z', 'A', 'R', 0, 3}, make([]byte, 44)...)} {
		if _, err := decryptArchive(buf, "passphrase"); err == nil {
			t.Fatal("invalid header accepted")
		}
	}
}
