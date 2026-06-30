permit (
    principal == ?principal,
    action in [
        HCP::Action::"CreateManagedHostedCluster",
        HCP::Action::"ListManagedHostedClusters",
        HCP::Action::"GetManagedHostedCluster",
        HCP::Action::"UpdateManagedHostedCluster",
        HCP::Action::"DeleteManagedHostedCluster",
        HCP::Action::"GetKubeConfig"
    ],
    resource in ?resource
);
