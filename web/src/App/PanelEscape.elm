module App.PanelEscape exposing (Open, Target(..), next)

{-| Which overlay Escape dismisses first.

Grid movement stays available when a docked inspector is merely open.
Escape still closes the front-most workflow panel before it exits loupe
or compare, so keyboard users are not stuck inside moderation or recovery.
-}


type Target
    = CloseImport
    | CloseUpload
    | CloseDuplicates
    | CloseSimilarity
    | CloseModeration
    | CloseRecovery
    | CloseDrawers
    | ExitMode
    | Idle


type alias Open =
    { browserImport : Bool
    , upload : Bool
    , duplicates : Bool
    , similarity : Bool
    , moderation : Bool
    , recovery : Bool
    , drawers : Bool
    , grid : Bool
    }


next : Open -> Target
next open =
    if open.browserImport then
        CloseImport

    else if open.upload then
        CloseUpload

    else if open.duplicates then
        CloseDuplicates

    else if open.similarity then
        CloseSimilarity

    else if open.moderation then
        CloseModeration

    else if open.recovery then
        CloseRecovery

    else if open.drawers then
        CloseDrawers

    else if not open.grid then
        ExitMode

    else
        Idle
