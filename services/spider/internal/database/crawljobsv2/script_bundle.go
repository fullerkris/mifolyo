package crawljobsv2

import (
	"errors"
	"io/fs"
	"strings"
	"unicode/utf8"
)

var (
	errCanonicalBundleInventory = errors.New("crawljobsv2: noncanonical Lua bundle inventory")
	errCanonicalBundleBytes     = errors.New("crawljobsv2: invalid canonical Lua source bytes")
)

// AuthoritativeScriptBindingSet returns the complete compile-time embedded Lua
// source binding. It accepts no source, hash, path, review, or transport inputs.
// All authority anchors are generated literals, not hashes trusted merely because
// they were computed at runtime. Each call returns fresh private bindings/seal.
//
// This establishes source identity ONLY. It does not approve a commit guard,
// accept Lua behavior or M4 evidence, authorize I/O, or activate the V1 client.
func AuthoritativeScriptBindingSet() (ScriptBindingSet, error) {
	return authoritativeScriptBindingSetFromFS(authoritativeLuaFS)
}

// ContractSHA256 validates the embedded source bundle and returns its fixed
// build-time contract pin. Runtime disk/document contents are not authority.
func ContractSHA256() (Digest, error) {
	if _, err := AuthoritativeScriptBindingSet(); err != nil {
		return "", err
	}
	return canonicalContractSHA256, nil
}

// Private seam for closed-inventory tests. Even here the caller cannot supply
// pins; alternative bytes must match every generated literal to be accepted.
func authoritativeScriptBindingSetFromFS(sources fs.FS) (ScriptBindingSet, error) {
	pins := canonicalScriptBindingPins()
	entries, err := fs.ReadDir(sources, "lua")
	if err != nil || len(entries) != len(pins) {
		return ScriptBindingSet{}, errCanonicalBundleInventory
	}
	expected := make(map[string]bool, len(pins))
	for _, pin := range pins {
		expected[pin.sourceName] = true
	}
	for _, entry := range entries {
		info, err := entry.Info()
		if err != nil || !entry.Type().IsRegular() || !info.Mode().IsRegular() || !expected[entry.Name()] {
			return ScriptBindingSet{}, errCanonicalBundleInventory
		}
		delete(expected, entry.Name())
	}
	if len(expected) != 0 {
		return ScriptBindingSet{}, errCanonicalBundleInventory
	}
	for index := range pins {
		source, err := fs.ReadFile(sources, "lua/"+pins[index].sourceName)
		if err != nil {
			return ScriptBindingSet{}, errCanonicalBundleInventory
		}
		pins[index].source = string(source)
	}
	set := ScriptBindingSet{
		bindings: pins[:], sourceSetSHA256: canonicalSourceSetSHA256,
		approvedContractSHA256: canonicalContractSHA256,
		seal: &scriptBindingSetSeal{
			sourceSetSHA256: canonicalSourceSetSHA256, approvedContractSHA256: canonicalContractSHA256,
			bundleSHA256: canonicalBundleSealSHA256,
		},
	}
	if err := validateCanonicalScriptBindingSet(set); err != nil {
		return ScriptBindingSet{}, err
	}
	return set, nil
}

func validateCanonicalScriptBindingSet(set ScriptBindingSet) error {
	pins := canonicalScriptBindingPins()
	if len(set.bindings) != len(pins) {
		return errCanonicalBundleInventory
	}
	for index, pin := range pins {
		binding := set.bindings[index]
		if binding.operation != pin.operation || binding.sourceName != pin.sourceName {
			return errCanonicalBundleInventory
		}
		if !utf8.ValidString(binding.source) || !strings.HasSuffix(binding.source, "\n") {
			return errCanonicalBundleBytes
		}
		if binding.redisSHA1 != pin.redisSHA1 || binding.sourceSHA256 != pin.sourceSHA256 {
			return ErrScriptBindingMismatch
		}
	}
	if set.seal == nil || set.sourceSetSHA256 != canonicalSourceSetSHA256 ||
		set.approvedContractSHA256 != canonicalContractSHA256 ||
		*set.seal != (scriptBindingSetSeal{canonicalSourceSetSHA256, canonicalContractSHA256, canonicalBundleSealSHA256}) {
		return ErrScriptBindingMismatch
	}
	if err := set.validate(); err != nil {
		return ErrScriptBindingMismatch
	}
	return nil
}
