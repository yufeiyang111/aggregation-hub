//go:build windows

package credential

import (
	"context"
	"errors"
	"fmt"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	credentialTypeGeneric         uint32 = 1
	credentialPersistLocalMachine uint32 = 2
)

var (
	advapi32   = windows.NewLazySystemDLL("advapi32.dll")
	credWrite  = advapi32.NewProc("CredWriteW")
	credRead   = advapi32.NewProc("CredReadW")
	credDelete = advapi32.NewProc("CredDeleteW")
	credFree   = advapi32.NewProc("CredFree")
)

type nativeCredential struct {
	Flags              uint32
	Type               uint32
	TargetName         *uint16
	Comment            *uint16
	LastWritten        windows.Filetime
	CredentialBlobSize uint32
	CredentialBlob     *byte
	Persist            uint32
	AttributeCount     uint32
	Attributes         unsafe.Pointer
	TargetAlias        *uint16
	UserName           *uint16
}
type WindowsStore struct{}

func NewWindowsStore() *WindowsStore { return &WindowsStore{} }
func (*WindowsStore) Put(ctx context.Context, ref Ref, value SecretValue) error {
	if ctx == nil {
		return ErrInvalidReference
	}
	target, err := ref.targetName()
	if err != nil {
		return err
	}
	if !value.valid() {
		return ErrInvalidSecret
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("编码凭据引用失败: %w", err)
	}
	credential := nativeCredential{Type: credentialTypeGeneric, TargetName: targetPtr, CredentialBlobSize: uint32(len(value.Bytes)), CredentialBlob: &value.Bytes[0], Persist: credentialPersistLocalMachine}
	if err := callCredential(credWrite, uintptr(unsafe.Pointer(&credential)), 0); err != nil {
		return fmt.Errorf("写入 Windows 凭据失败: %w", err)
	}
	return nil
}
func (*WindowsStore) Get(ctx context.Context, ref Ref) (SecretValue, error) {
	if ctx == nil {
		return SecretValue{}, ErrInvalidReference
	}
	target, err := ref.targetName()
	if err != nil {
		return SecretValue{}, err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return SecretValue{}, fmt.Errorf("编码凭据引用失败: %w", err)
	}
	var credential *nativeCredential
	if err := callCredential(credRead, uintptr(unsafe.Pointer(targetPtr)), uintptr(credentialTypeGeneric), 0, uintptr(unsafe.Pointer(&credential))); err != nil {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return SecretValue{}, ErrNotFound
		}
		return SecretValue{}, fmt.Errorf("读取 Windows 凭据失败: %w", err)
	}
	defer credFree.Call(uintptr(unsafe.Pointer(credential)))
	if credential.CredentialBlobSize == 0 || credential.CredentialBlob == nil {
		return SecretValue{}, ErrInvalidSecret
	}
	data := unsafe.Slice(credential.CredentialBlob, int(credential.CredentialBlobSize))
	return SecretValue{Bytes: append([]byte(nil), data...)}, nil
}
func (*WindowsStore) Delete(ctx context.Context, ref Ref) error {
	if ctx == nil {
		return ErrInvalidReference
	}
	target, err := ref.targetName()
	if err != nil {
		return err
	}
	targetPtr, err := windows.UTF16PtrFromString(target)
	if err != nil {
		return fmt.Errorf("编码凭据引用失败: %w", err)
	}
	if err := callCredential(credDelete, uintptr(unsafe.Pointer(targetPtr)), uintptr(credentialTypeGeneric), 0); err != nil {
		if errors.Is(err, windows.ERROR_NOT_FOUND) {
			return ErrNotFound
		}
		return fmt.Errorf("删除 Windows 凭据失败: %w", err)
	}
	return nil
}
func (*WindowsStore) Probe(context.Context) Status {
	return Status{Available: true, Backend: "windows_credential_manager"}
}
func callCredential(proc *windows.LazyProc, args ...uintptr) error {
	result, _, err := proc.Call(args...)
	if result == 0 {
		return err
	}
	return nil
}
