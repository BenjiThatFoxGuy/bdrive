import { memo } from "react";
import { Outlet, getRouteApi } from "@tanstack/react-router";
import { Button } from "@heroui/react";
import CodiconAccount from "~icons/codicon/account";
import CodiconSettings from "~icons/codicon/settings";
import FluentDarkTheme20Filled from "~icons/fluent/dark-theme-20-filled";
import IcOutlineInfo from "~icons/ic/outline-info";
import MaterialSymbolsScheduleRounded from "~icons/material-symbols/schedule-rounded";
import clsx from "clsx";

import { ForwardLink } from "@/components/forward-link";
import { motion } from "framer-motion";

const Tabs = [
  {
    icon: CodiconSettings,
    id: "general",
  },
  {
    icon: FluentDarkTheme20Filled,
    id: "appearance",
  },
  {
    icon: CodiconAccount,
    id: "account",
  },
  {
    icon: MaterialSymbolsScheduleRounded,
    id: "jobs",
  },
  {
    icon: IcOutlineInfo,
    id: "info",
  },
];

const fileRoute = getRouteApi("/_authed/settings/$tabId");

export const Settings = memo(() => {
  const params = fileRoute.useParams();

  return (
    <div className="bg-surface/50 mx-auto size-full rounded-xl flex flex-col md:flex-row max-w-5xl gap-6 p-4">
      <div className="flex flex-col gap-4 w-full md:w-1/4">
        <h1 className="text-2xl font-semibold pt-2 px-2">Settings</h1>
        <nav className="flex flex-row md:flex-col gap-1 overflow-x-auto no-scrollbar">
          {Tabs.map((tab) => (
            <div key={tab.id} className="relative w-auto md:w-full shrink-0">
              <ForwardLink
                to="/settings/$tabId"
                data-selected={params.tabId === tab.id}
                replace
                params={{ tabId: tab.id }}
                className={clsx(
                  "text-inherit h-14 w-full justify-center md:justify-start px-4 z-1 inline-flex items-center",
                  "data-[hover=true]:text-foreground text-muted",
                  "data-[selected=true]:text-foreground data-[hover=true]:bg-transparent",
                  "[&>span>svg]:data-[hover=true]:scale-110 ",
                  "[&>span>svg]:data-[selected=true]:scale-110",
                  "flex-col md:flex-row items-center",
                  "gap-1 md:gap-2",
                )}
              >
                <span className="capitalize text-xs md:text-base">{tab.id}</span>
              </ForwardLink>
              {params.tabId === tab.id && (
                <motion.span
                  className="absolute rounded-full inset-x-1 bottom-0 h-1 md:inset-y-1 md:left-0 md:right-0 md:h-auto z-0 bg-accent-soft text-accent-soft-foreground"
                  layoutId="pill"
                  transition={{
                    bounce: 0.1,
                    duration: 0.4,
                    type: "spring",
                  }}
                />
              )}
            </div>
          ))}
        </nav>
      </div>
      <div className="flex-1 overflow-hidden">
        <Outlet />
      </div>
    </div>
  );
});
