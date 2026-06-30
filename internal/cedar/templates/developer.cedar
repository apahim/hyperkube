permit (
    principal == ?principal,
    action in [
        HCP::Action::"ListManagedHostedClusters",
        HCP::Action::"GetManagedHostedCluster",
        HCP::Action::"GetKubeConfig"
    ],
    resource in ?resource
);
