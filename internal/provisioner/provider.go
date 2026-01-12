package provisioner

type ClusterProvider interface {
	Provision() error
	InstallAddons() error
}
