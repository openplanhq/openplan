import AuthCard, { AuthCardTitle } from "./AuthCard";
import { Button } from "@/components/ui/button";
import { CardContent, CardHeader } from "@/components/ui/card";

// Sign-in worked but the session didn't stick: shown both by SessionProvider,
// when it sees the login loop, and by SignInScreen, when the loop brings a
// person back to it. One card so the advice reads the same from either side.
export default function CookiesBlockedCard({ onRetry, testId, retryTestId }: { onRetry: () => void; testId: string; retryTestId: string }) {
  return (
    <AuthCard data-testid={testId}>
      <CardHeader>
        <AuthCardTitle>We could not keep you signed in</AuthCardTitle>
      </CardHeader>
      <CardContent className="grid gap-4">
        <p className="text-muted-foreground">
          Sign-in worked, but this browser did not hold on to the session, so every page load
          started it over. That happens when cookies are blocked for this site — by browser
          settings, an extension, or a privacy mode that clears them between page loads.
        </p>
        <p className="text-muted-foreground">
          Allow cookies for this site and try again. If it keeps failing, contact your
          administrator.
        </p>
        <Button className="w-full pointer-coarse:h-11" onClick={onRetry} data-testid={retryTestId}>
          Try again
        </Button>
      </CardContent>
    </AuthCard>
  );
}
