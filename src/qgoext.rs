use zed_extension_api::{
    self as zed, settings::LspSettings, Architecture, Command, DownloadedFileType,
    GithubReleaseOptions, LanguageServerId, Os, Result, Worktree,
};

const SERVER_NAME: &str = "qgoext";
const GITHUB_REPO: &str = "jorgefuertes/qgoext";
const PROXY_BIN: &str = "qgoext-proxy";

struct Qgoext {
    cached_proxy_path: Option<String>,
}

impl zed::Extension for Qgoext {
    fn new() -> Self {
        Self {
            cached_proxy_path: None,
        }
    }

    fn language_server_command(
        &mut self,
        _server_id: &LanguageServerId,
        worktree: &Worktree,
    ) -> Result<Command> {
        let env = worktree.shell_env();
        let binary_settings = LspSettings::for_worktree(SERVER_NAME, worktree)
            .ok()
            .and_then(|s| s.binary);
        let user_args = binary_settings
            .as_ref()
            .and_then(|b| b.arguments.clone())
            .unwrap_or_default();

        if let Some(path) = binary_settings.and_then(|b| b.path) {
            return Ok(Command {
                command: path,
                args: user_args,
                env,
            });
        }

        let gopls = worktree.which("gopls").ok_or_else(|| {
            "gopls not found on PATH. Install with: go install golang.org/x/tools/gopls@latest"
                .to_string()
        })?;

        let proxy = self.resolve_proxy(worktree)?;

        let mut args = vec!["--gopls".to_string(), gopls];
        args.extend(user_args);

        Ok(Command {
            command: proxy,
            args,
            env,
        })
    }
}

impl Qgoext {
    fn resolve_proxy(&mut self, worktree: &Worktree) -> Result<String> {
        if let Some(path) = self.cached_proxy_path.clone() {
            return Ok(path);
        }
        if let Some(path) = worktree.which(PROXY_BIN) {
            self.cached_proxy_path = Some(path.clone());
            return Ok(path);
        }
        self.download_proxy()
    }

    fn download_proxy(&mut self) -> Result<String> {
        let release = zed::latest_github_release(
            GITHUB_REPO,
            GithubReleaseOptions {
                require_assets: true,
                pre_release: false,
            },
        )?;

        let (os, arch) = zed::current_platform();
        let suffix = match (os, arch) {
            (Os::Mac, Architecture::Aarch64) => "aarch64-apple-darwin.tar.gz",
            (Os::Mac, Architecture::X8664) => "x86_64-apple-darwin.tar.gz",
            (Os::Linux, Architecture::Aarch64) => "aarch64-unknown-linux-gnu.tar.gz",
            (Os::Linux, Architecture::X8664) => "x86_64-unknown-linux-gnu.tar.gz",
            (Os::Windows, Architecture::X8664) => "x86_64-pc-windows-msvc.zip",
            _ => return Err("unsupported platform/arch for qgoext-proxy".to_string()),
        };

        let asset_name = format!("{PROXY_BIN}-{suffix}");
        let file_type = if os == Os::Windows {
            DownloadedFileType::Zip
        } else {
            DownloadedFileType::GzipTar
        };

        let version_dir = format!("{PROXY_BIN}-{}", release.version);
        let bin_name = if os == Os::Windows {
            format!("{PROXY_BIN}.exe")
        } else {
            PROXY_BIN.to_string()
        };
        let binary_path = format!("{version_dir}/{bin_name}");

        if !std::path::Path::new(&binary_path).exists() {
            let asset = release
                .assets
                .into_iter()
                .find(|a| a.name == asset_name)
                .ok_or_else(|| format!("asset {asset_name} not in release {}", release.version))?;

            zed::download_file(&asset.download_url, &version_dir, file_type)?;
            zed::make_file_executable(&binary_path)?;
            remove_outdated_versions(&version_dir)?;
        }

        self.cached_proxy_path = Some(binary_path.clone());
        Ok(binary_path)
    }
}

fn remove_outdated_versions(keep: &str) -> Result<()> {
    let Ok(entries) = std::fs::read_dir(".") else {
        return Ok(());
    };
    for entry in entries.flatten() {
        let name = entry.file_name().to_string_lossy().into_owned();
        if name.starts_with(&format!("{PROXY_BIN}-")) && name != keep {
            let _ = std::fs::remove_dir_all(entry.path());
        }
    }
    Ok(())
}

zed::register_extension!(Qgoext);
