package provisioner

type ClusterProvider interface {
	CheckSystemRequirements() error
	Provision() error
	InstallAddons() error
}
