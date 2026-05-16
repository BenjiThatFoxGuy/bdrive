import { memo, type ReactNode } from "react";
import { motion } from "framer-motion";

interface SettingsSectionProps {
  title: string;
  description?: string;
  children?: ReactNode;
}

export const SettingsSection = memo(
  ({ title, description, children }: SettingsSectionProps) => {
    return (
      <motion.div
        initial={{ opacity: 0, y: 10 }}
        animate={{ opacity: 1, y: 0 }}
        transition={{ duration: 0.3 }}
        className="bg-surface rounded-2xl p-5 shadow-sm hover:shadow-md transition-shadow duration-300 border border-border/50 flex flex-col gap-4"
      >
        <div className="flex items-start gap-3">
          <div className="p-2.5 rounded-xl bg-accent/10">
            <h3 className="text-lg font-semibold">{title}</h3>
          </div>
          <div className="flex-1 min-w-0">
            {description && (
              <p className="text-sm text-muted">{description}</p>
            )}
          </div>
        </div>
        {children && <div className="space-y-4">{children}</div>}
      </motion.div>
    );
  },
);
