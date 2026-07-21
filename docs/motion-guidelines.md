# Motion guidelines

The implementation begins in Phase 1. The governing constraints are already
fixed:

- motion communicates navigation, state change, or real/mock event arrival;
- no ambient or particle animation under `prefers-reduced-motion`;
- no continuously animated table rows, full-screen blur, or large box shadow;
- LiveFlow emits particles only when an event arrives and caps active desktop
  particles at 24;
- background animation pauses while the document is hidden;
- loading, approval, navigation, and errors are never delayed by animation.

Phase 1 will add measured duration/easing tokens and screenshot baselines here.
