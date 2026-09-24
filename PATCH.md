# Local patch

Baseline: `github.com/zhaarey/go-mp4tag@v0.0.0-20260509131819-a89fa417cd97`

Changes:

- Accept `stco` or `co64` when validating a file for tag writes.
- Update every `moov.trak.mdia.minf.stbl.stco` table when tag/cover growth
  moves `mdat`.
- Update every `moov.trak.mdia.minf.stbl.co64` table for 64-bit chunk offsets.
- Reject 32-bit overflow and negative underflow instead of wrapping offsets.
- Add unit coverage for multiple tracks, `co64`, and invalid offsets.
- Parse box sizes per ISO/IEC 14496-12: read the 32-bit size as unsigned
  (boxes of 2 GiB-4 GiB no longer seek before the start of the file),
  support 64-bit `largesize` (size 1) and size 0 (box extends to the end of
  its parent), and reject sizes smaller than the header instead of looping.
- Refuse to resize `moov`/`udta`/`meta`/`ilst` in place when they use a
  64-bit size header or would outgrow 32 bits, rather than corrupting them.
- Read and write the classical work/movement atoms: `©wrk` (Work) and
  `©mvn` (Movement) as UTF-8 text, `©mvi` (MovementIndex) and `©mvc`
  (MovementCount) as 2-byte type 21 integers, and `shwm`
  (ShowWorkAndMovement) as a 1-byte type 21 flag.
- When the chunk offset tables are stored after `ilst` (metadata before
  `trak`), patch them at their relocated position after copying the rest
  of the file, instead of at their old position where the copy overwrote them.
