package provisioner

type ClusterProvider interface {
	PreFlightChecks() error
	Provision() error
	InstallAddons() error
}
