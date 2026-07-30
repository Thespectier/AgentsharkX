import { AnimatePresence, motion, useReducedMotion } from "motion/react";

import { SeverityBadge, SourceBadge, cn } from "../components/ui";
import { formatTimeWithZone, productizeText } from "../lib/format";
import { useI18n } from "../lib/i18n";
import type { UnifiedEvent } from "../types";

export function ActivityRail({ events, limit = 6 }: { events: UnifiedEvent[]; limit?: number }) {
  const reduced = useReducedMotion();
  const { t } = useI18n();
  return (
    <div className="activity-rail">
      <AnimatePresence initial={false}>
        {events.slice(0, limit).map((event, index) => (
          <motion.article
            animate={{ opacity: 1, y: 0 }}
            className={cn("activity-item", index === 0 && "activity-item--new")}
            exit={{ opacity: 0, height: 0 }}
            initial={reduced ? false : { opacity: 0, y: -10 }}
            key={event.id}
            layout={!reduced}
            transition={{ duration: 0.28 }}
          >
            <span className={cn("activity-item__line", `activity-item__line--${event.severity}`)} />
            <div className="activity-item__body">
              <div className="activity-item__meta">
                <SourceBadge source={event.source} />
                <span>{formatTimeWithZone(event.timestamp)}</span>
              </div>
              <p>{productizeText(event.summary)}</p>
              <div className="activity-item__footer">
                <span>{t(event.phase ?? event.kind)}</span>
                {event.decision ? (
                  <strong>{t(event.decision)}</strong>
                ) : (
                  <SeverityBadge severity={event.severity} />
                )}
              </div>
            </div>
          </motion.article>
        ))}
      </AnimatePresence>
    </div>
  );
}
