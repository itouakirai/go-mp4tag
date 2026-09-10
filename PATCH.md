# Local patch

Baseline: `github.com/zhaarey/go-mp4tag@v0.0.0-20260509131819-a89fa417cd97`

Changes:

- Accept `stco` or `co64` when validating a file for tag writes.
- Update every `moov.trak.mdia.minf.stbl.stco` table when tag/cover growth
  moves `mdat`.
- Update every `moov.trak.mdia.minf.stbl.co64` table for 64-bit chunk offsets.
- Reject 32-bit overflow and negative underflow instead of wrapping offsets.
- Add unit coverage for multiple tracks, `co64`, and invalid offsets.