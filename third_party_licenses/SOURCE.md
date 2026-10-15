# Pinned notices for npm packages without standalone LICENSE files

The release builder copies a notice from each installed package when one is
present. The four exact package versions below omit a standalone LICENSE file.
The builder verifies the SHA-256 of these alternate upstream texts before
including them in `licenses/` and records the copied file in `MANIFEST.tsv`.

| Package | Notice source | SHA-256 | Verification |
| --- | --- | --- | --- |
| `@ant-design/icons-svg@4.6.0` | [Ant Design Icons repository LICENSE at commit `7f2516ac91226d2b41f93b35cb5197c8d94f7189`](https://github.com/ant-design/ant-design-icons/blob/7f2516ac91226d2b41f93b35cb5197c8d94f7189/LICENSE) | `5d367fb0a07340571542eb4ee8eb1add62d37b71bad6786a57c2e9a86bf76c70` | The package declares MIT. No `4.6.0` repository tag exists; this pinned root notice matches the earlier official `@ant-design/icons-svg@4.0.0` tag byte for byte. |
| `agent-base@6.0.2` | [Tagged upstream README license section](https://github.com/TooTallNate/node-agent-base/blob/6.0.2/README.md#license) | `f1425c3b72330fe4fb2aa5a2fb152e939bdf534692a32b5f0b38f74147b98556` | Installed npm README matches the tagged upstream README byte for byte. |
| `https-proxy-agent@5.0.1` | [Tagged upstream README license section](https://github.com/TooTallNate/node-https-proxy-agent/blob/5.0.1/README.md#license) | `32f0856d2c43df7d05cca960fdee84e1e38ab545bd7b2186433dfa41aa90a712` | Installed npm README matches the tagged upstream README byte for byte. |
| `is-mobile@5.0.0` | [Tagged upstream README license section](https://github.com/juliangruber/is-mobile/blob/v5.0.0/README.md#license) | `1f1f35b6a103d7b010f51a6d7d61e3e47dbc71db2c69cbb7c0ffd40c641b04e2` | Installed npm README matches the tagged upstream README byte for byte. |

The pinned Ant Design Icons LICENSE is stored as
`third_party_licenses/ant-design-icons-LICENSE`; the three tagged READMEs are
copied from the lockfile-installed dependency tree. An unknown missing notice
causes packaging to fail so that a dependency update receives a fresh review.
