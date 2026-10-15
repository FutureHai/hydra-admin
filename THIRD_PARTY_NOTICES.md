# Third-party notices

The Hydra Admin Console code is MIT licensed (see `LICENSE`). Its Go module graph, bundled frontend dependencies, and pinned ORY Hydra OpenAPI material keep their own licenses.

Each release's `licenses/MANIFEST.tsv` records the exact dependency name, locked version, declared npm license identifier where available, and copied upstream license/notice text file. The build copies these files from the Go modules actually linked into `./cmd/hydra-admin` (`go list -deps -json`) and installed production npm packages. Three npm packages carry their complete MIT notice in a tagged README; one uses a pinned official repository LICENSE because the npm tarball has no standalone notice. Sources and SHA-256 values are in `licenses/UPSTREAM_SOURCES.md`. Packaging fails if any dependency notice is still missing. Optional packages absent on the build platform are not copied because they are not present in the Linux release. The shipped browser bundle is built from the installed dependency tree.

The pinned ORY Hydra `v26.2.0` OpenAPI material is Apache-2.0 licensed. Its source URL, download date and SHA-256 are in `specs/v26.2.0/SOURCE.md`; the [adapter review](https://github.com/FutureHai/hydra-admin-doc/blob/master/specs/v26.2.0/ADAPTER_REVIEW.md) records the endpoint boundary. Its license text is copied as `licenses/ORY_HYDRA_OPENAPI_LICENSE`.

Do not interpret this software's MIT license as relicensing any dependency or ORY material. Review the generated manifest and pinned upstream sources as part of the public release process.
