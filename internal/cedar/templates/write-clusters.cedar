permit (
    principal == ?principal,
    action == HCP::Action::"WriteManagedHostedCluster",
    resource in ?resource
);
