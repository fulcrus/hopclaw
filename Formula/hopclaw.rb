class Hopclaw < Formula
  desc "Tool-using agent runtime and gateway"
  homepage "https://github.com/fulcrus/hopclaw"
  license "Apache-2.0"
  head "https://github.com/fulcrus/hopclaw.git", branch: "main"

  depends_on "go" => :build

  def install
    ldflags = %W[
      -s
      -w
      -X github.com/fulcrus/hopclaw/internal/version.Version=head
      -X github.com/fulcrus/hopclaw/internal/version.BuildDate=unknown
    ]

    system "go", "build", *std_go_args(output: bin/"hopclaw", ldflags: ldflags), "./cmd/hopclaw"
  end

  test do
    output = shell_output("#{bin}/hopclaw version")
    assert_match "head", output
  end
end
