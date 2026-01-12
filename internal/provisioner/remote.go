package provisioner

import (
	_ "embed"
)

//go:embed manifests/remote-config.yaml
var defaultRemoteConfig []byte

type RemoteProvider struct {
	Name string
}

func (k *RemoteProvider) Provision() error {
	return nil
}

func (k *RemoteProvider) InstallAddons() error {
	return nil
}
