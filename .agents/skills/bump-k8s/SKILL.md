# Bump Kubernetes Dependencies

Follow the instructions in [docs/development/bump-k8s.md](../../../docs/development/bump-k8s.md) exactly, using `$ARGUMENTS` as the target K8s minor or patch version (e.g. `1.37` or `1.37.1`). Include the separate E2E module, E2E cluster versions and VM images, and the Devbox kubectl lock entry. Verify provider and image compatibility before changing E2E defaults; evaluate template version guards rather than replacing them mechanically.

When checking available VM images through Prism Central APIs, use v4 only (never v3), without printing credentials. Repo image defaults are not proof of availability. Keep the existing image and matching workload version until a suitable newer image is confirmed.
