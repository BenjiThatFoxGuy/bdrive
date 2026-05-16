import { Link } from "@tanstack/react-router";

import { siteConfig } from "@/config/site";
import { cn } from "@/lib/utils";

export const SideNav = ({ className }: { className?: string }) => {
  return (
    <aside
      className={cn(
        "w-full h-20 md:w-24 md:h-full shrink-0 flex flex-row md:flex-col items-center justify-around md:justify-start md:pt-16 z-20 overflow-x-auto md:overflow-y-auto scrollbar-hide",
        className,
      )}
    >
      {siteConfig.navItems.map((item) => (
        <Link
          key={item.id}
          {...item.options}
          preload="intent"
          className="flex flex-col items-center gap-1 md:gap-2 md:mb-8 group relative"
        >
          {({ isActive }) => (
            <>
              <div
                className={cn(
                  "relative flex items-center justify-center size-10 md:size-12 rounded-xl transition-all duration-300 group-hover:text-accent",
                  isActive && "text-accent shadow-glow",
                )}
              >
                {isActive && (
                  <div className="absolute -bottom-2 md:-bottom-auto md:-left-6 left-1/2 -translate-x-1/2 md:translate-x-0 top-auto md:top-1/2 md:-translate-y-1/2 w-8 h-1 md:w-1 md:h-8 bg-accent rounded-full shadow-[0_0_10px_var(--accent)]"></div>
                )}
                <div className={cn("h-5 w-5 md:h-6 md:w-6")}>
                  {isActive ? <item.activeIcon /> : <item.icon />}
                </div>
              </div>

              <span
                className={cn(
                  "text-label md:text-[10px] font-black transition-all duration-300 uppercase tracking-widest group-hover:text-accent",
                  isActive && "text-accent",
                )}
              >
                {item.label}
              </span>
            </>
          )}
        </Link>
      ))}
    </aside>
  );
};
