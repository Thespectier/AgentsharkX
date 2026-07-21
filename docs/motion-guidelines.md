# Motion guidelines

Phase 1 implements the following motion tokens:

| Token | Value | Use |
|---|---:|---|
| `--motion-fast` | 140 ms | hover and focus feedback |
| `--motion-normal` | 240 ms | local state transitions |
| `--motion-enter` | 420 ms | page, dialog, and drawer entry |
| `--motion-ambient` | 16 s | low-contrast background grid only |

The governing constraints are:

- motion communicates navigation, state change, or real/mock event arrival;
- no ambient or particle animation under `prefers-reduced-motion`;
- no continuously animated table rows, full-screen blur, or large box shadow;
- LiveFlow emits particles only when an event arrives and caps active desktop
  particles at 24;
- background animation pauses while the document is hidden;
- loading, approval, navigation, and errors are never delayed by animation.

`useReducedMotion()` removes Motion entry/layout animation, the LiveFlow emits
no particles, and the CSS media query disables every continuous animation.
When the document is hidden, the ambient grid is explicitly paused while the
bounded Mock SSE stream may continue low-frequency synchronization.

Playwright verifies reduced-motion behavior and disables animation during
pixel comparison. The reviewed baselines are listed in
[screenshots/README.md](screenshots/README.md).
