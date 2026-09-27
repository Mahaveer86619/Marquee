"""Entry point. Serves a health endpoint until the gRPC service is implemented."""

import os

from marquee_audiolab import __version__
from marquee_audiolab.health import serve

if __name__ == "__main__":
    serve("audiolab", __version__, int(os.environ.get("MARQUEE_AUDIOLAB_PORT", "7710")))
