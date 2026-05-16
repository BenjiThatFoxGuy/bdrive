export const ErrorView = ({ message }: { message: string }) => {
  return (
    <div className="m-auto mt-16 flex rounded-xl max-w-md min-h-48 flex-col justify-center items-center bg-surface gap-4 px-4">
      <h1 className="text-2xl font-bold text-foreground">Error</h1>
      <p className="text-lg text-foreground capitalize">{message}</p>
    </div>
  );
};
