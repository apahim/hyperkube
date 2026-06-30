permit (
    principal == ?principal,
    action in [
        HCP::Action::"ListManagedHostedClusters",
        HCP::Action::"GetManagedHostedCluster"
    ],
    resource in ?resource
);
