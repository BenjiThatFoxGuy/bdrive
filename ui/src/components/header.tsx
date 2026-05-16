import { type ChangeEvent, memo, useCallback, useEffect, useRef, useState } from "react";
import { useQueryClient } from "@tanstack/react-query";
import { Link, useNavigate } from "@tanstack/react-router";
import { Button, Input } from "@heroui/react";
import IconBiSearch from "~icons/bi/search";
import MdiFilterOutline from "~icons/mdi/filter-outline";
import PhTelegramLogoFill from "~icons/ph/telegram-logo-fill";
import clsx from "clsx";
import debounce from "lodash.debounce";

import { ProfileDropDown } from "./menus/profile";
import { SearchMenu } from "./menus/search/search";
import { ThemeToggle } from "./menus/theme-toggle";

const cleanSearchInput = (input: string) => input.trim().replace(/\s+/g, " ");

interface SearchBarProps {
  className?: string;
}

const SearchBar = memo(({ className }: SearchBarProps) => {
  const [query, setQuery] = useState("");

  const [isOpen, setIsOpen] = useState(false);

  const triggerRef = useRef<HTMLButtonElement | null>(null);

  const queryClient = useQueryClient();

  const navigate = useNavigate();

  const debouncedSearch = useCallback(
    debounce(
      (newValue: string) =>
        navigate({
          to: "/$view",
          search: {
            query: newValue,
          },
          params: {
            view: "search",
          },
          replace: true,
        }),
      1000,
    ),
    [],
  );

  useEffect(() => {
    const handleKeyDown = (e: KeyboardEvent) => {
      if ((e.ctrlKey || e.metaKey) && e.key === "k") {
        e.preventDefault();
        setIsOpen((prev) => !prev);
      }
    };

    window.addEventListener("keydown", handleKeyDown);
    return () => window.removeEventListener("keydown", handleKeyDown);
  }, []);

  const updateQuery = useCallback((event: ChangeEvent<HTMLInputElement>) => {
    setQuery(event.target.value);
    const cleanInput = cleanSearchInput(event.target.value);
    if (cleanInput) {
      queryClient.cancelQueries({
        queryKey: ["files"],
      });
      debouncedSearch(cleanInput);
    }
  }, []);

  return (
    <>
      <div className={clsx("relative min-w-[15rem] max-w-96", className)}>
        <IconBiSearch className="absolute left-3 top-1/2 -translate-y-1/2 size-6 text-muted pointer-events-none" />
        <Input
          variant="secondary"
          placeholder="Search... (Ctrl+K)"
          enterKeyHint="search"
          autoComplete="off"
          aria-label="search"
          value={query}
          onChange={updateQuery}
          className="min-w-[15rem] max-w-96 pl-10 pr-10"
        />
        <Button
          isIconOnly
          variant="ghost"
          size="md"
          ref={triggerRef}
          className="absolute right-1 top-1/2 -translate-y-1/2 size-8 min-w-8 text-current"
          onPress={() => setIsOpen((val) => !val)}
        >
          <MdiFilterOutline />
        </Button>
      </div>
      {isOpen && <SearchMenu isOpen={isOpen} setIsOpen={setIsOpen} />}
    </>
  );
});

export default memo(function Header({ auth }: { auth?: boolean }) {
  return (
    <header className="sticky top-0 z-50 flex items-center min-h-12 xs:min-h-16 px-4">
      <div className="flex-1 flex gap-2 items-center">
        <Link
          to="/$view"
          params={{ view: "my-drive" }}
          search={{ path: "/" }}
          className="flex gap-2 items-center cursor-pointer"
        >
          <PhTelegramLogoFill className="size-6 text-inherit" />
          <p className="text-2xl hidden sm:block">Teldrive</p>
        </Link>
      </div>
      <div className="flex-1 flex justify-end items-center gap-4">
        {auth && <SearchBar className="hidden xs:block" />}
        <ThemeToggle />
        {auth && <ProfileDropDown />}
      </div>
    </header>
  );
});
