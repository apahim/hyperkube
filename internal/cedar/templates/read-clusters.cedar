permit (
    principal == ?principal,
    action == HCP::Action::"ReadManagedHostedCluster",
    resource in ?resource
);
