//go:build !windows

package credential

type WindowsStore struct{}

func NewWindowsStore() *WindowsStore                              { return &WindowsStore{} }
func (*WindowsStore) Put(context.Context, Ref, SecretValue) error { return ErrUnsupported }
func (*WindowsStore) Get(context.Context, Ref) (SecretValue, error) {
	return SecretValue{}, ErrUnsupported
}
func (*WindowsStore) Delete(context.Context, Ref) error { return ErrUnsupported }
func (*WindowsStore) Probe(context.Context) Status {
	return Status{Available: false, Backend: "unsupported", Detail: ErrUnsupported.Error()}
}
