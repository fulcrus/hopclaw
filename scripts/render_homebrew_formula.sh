#!/bin/sh

set -eu

if [ "$#" -ne 4 ]; then
	echo "usage: $0 <version> <repository> <checksums.txt> <output>" >&2
	exit 1
fi

version="$(printf '%s\n' "$1" | sed 's/^v//')"
tag="v$version"
repository="$2"
checksums_file="$3"
output_file="$4"

checksum_for() {
	asset="$1"
	awk -v asset="$asset" '
		{
			file=$2
			sub(/^\*/, "", file)
			if (file == asset) {
				print $1
				exit
			}
		}
	' "$checksums_file"
}

darwin_amd64_asset="hopclaw_${version}_darwin_amd64.tar.gz"
darwin_arm64_asset="hopclaw_${version}_darwin_arm64.tar.gz"
linux_amd64_asset="hopclaw_${version}_linux_amd64.tar.gz"
linux_arm64_asset="hopclaw_${version}_linux_arm64.tar.gz"

darwin_amd64_sha="$(checksum_for "$darwin_amd64_asset")"
darwin_arm64_sha="$(checksum_for "$darwin_arm64_asset")"
linux_amd64_sha="$(checksum_for "$linux_amd64_asset")"
linux_arm64_sha="$(checksum_for "$linux_arm64_asset")"

for value in "$darwin_amd64_sha" "$darwin_arm64_sha" "$linux_amd64_sha" "$linux_arm64_sha"; do
	if [ -z "$value" ]; then
		echo "missing checksum in $checksums_file" >&2
		exit 1
	fi
done

cat >"$output_file" <<EOF
class Hopclaw < Formula
  desc "Tool-using agent runtime and gateway"
  homepage "https://github.com/$repository"
  version "$version"
  license "Apache-2.0"

  on_macos do
    if Hardware::CPU.arm?
      url "https://github.com/$repository/releases/download/$tag/$darwin_arm64_asset"
      sha256 "$darwin_arm64_sha"
    else
      url "https://github.com/$repository/releases/download/$tag/$darwin_amd64_asset"
      sha256 "$darwin_amd64_sha"
    end
  end

  on_linux do
    if Hardware::CPU.arm?
      url "https://github.com/$repository/releases/download/$tag/$linux_arm64_asset"
      sha256 "$linux_arm64_sha"
    else
      url "https://github.com/$repository/releases/download/$tag/$linux_amd64_asset"
      sha256 "$linux_amd64_sha"
    end
  end

  def install
    root = Dir["*"].find { |path| File.directory?(path) } || "."

    bin.install "#{root}/hopclaw"
    bin.install "#{root}/openclaw"
    bin.install "#{root}/hopclaw-browserd"
    bin.install "#{root}/hopclaw-desktopd"
    bin.install "#{root}/hopclaw-gateway"
    doc.install "#{root}/README.md", "#{root}/README.zh-CN.md", "#{root}/CHANGELOG.md", "#{root}/SECURITY.md", "#{root}/LICENSE", "#{root}/NOTICE"
  end

  test do
    output = shell_output("#{bin}/hopclaw version")
    assert_match version.to_s, output
  end
end
EOF
