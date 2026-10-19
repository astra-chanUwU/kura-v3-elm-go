port module App.LeaveGuard exposing (profileDirty)

{-| Asks the page to warn before unload while a profile draft is unsaved. -}
port profileDirty : Bool -> Cmd msg
