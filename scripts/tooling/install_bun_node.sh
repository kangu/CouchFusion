# Bun as an alternative to look into
echo "Installing Bun..."

curl -fsSL https://bun.sh/install | bash
source ~/.bashrc
EXPORT BUN_INSTALL="$HOME/.bun"
EXPORT PATH="$BUN_INSTALL/bin:$PATH"

echo "✅ Bun is ready."

curl -o- https://raw.githubusercontent.com/nvm-sh/nvm/v0.40.3/install.sh | bash
export NVM_DIR="$([ -z "${XDG_CONFIG_HOME-}" ] && printf %s "${HOME}/.nvm" || printf %s "${XDG_CONFIG_HOME}/nvm")"
[ -s "$NVM_DIR/nvm.sh" ] && \. "$NVM_DIR/nvm.sh" # This loads nvm

# As of 2024-04-12
nvm install 24

  echo "✅ NodeJS is ready."
