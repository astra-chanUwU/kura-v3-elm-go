module App.Access exposing
    ( State
    , Status(..)
    , blankTokenNotice
    , canWrite
    , credential
    , discovered
    , draftChanged
    , init
    , isCurrent
    , lockRequested
    , retryRequested
    , revoked
    , unlockRequested
    , validated
    )

{-| Transient write-access state for the browser unlock/lock flow.

The token, its pending validation copy, and the field draft live only in
this record: they are never written to preferences, localStorage, URLs, or
flags, and drafts clear on validation while everything clears on lock.
`generation` invalidates credential-dependent responses that arrive after a
lock, unlock, or retry. Callers ignore obsolete access/owner state and reconcile
public metadata when an old mutation settles. Fixed notice strings never interpolate the token or draft,
so a wrong token cannot leak through an error message.
-}

import Api.Access exposing (Capabilities)
import Http


type Status
    = Checking
    | OpenLocal
    | Locked
    | Validating
    | Unlocked
    | Unavailable


type alias State =
    { status : Status
    , gated : Bool
    , token : Maybe String
    , pending : Maybe String
    , draft : String
    , generation : Int
    , notice : Maybe String
    }


init : State
init =
    { status = Checking
    , gated = True
    , token = Nothing
    , pending = Nothing
    , draft = ""
    , generation = 0
    , notice = Nothing
    }


{-| Writes are enabled only in the two confirmed states. Unknown,
validating, locked, and unavailable states all disable writes while
browsing stays available.
-}
canWrite : State -> Bool
canWrite state =
    case state.status of
        OpenLocal ->
            True

        Unlocked ->
            True

        _ ->
            False


{-| The token to attach to mutations and owner-scoped reads. Present only
while unlocked; open-local mode needs no credential.
-}
credential : State -> Maybe String
credential state =
    case state.status of
        Unlocked ->
            state.token

        _ ->
            Nothing


{-| A completion belongs to the current access epoch when its generation
matches. Stale validation and saved-search reads are dropped; old mutation
completions may trigger fresh reads without restoring credentials or notices.
-}
isCurrent : Int -> State -> Bool
isCurrent generation state =
    generation == state.generation


blankTokenNotice : String
blankTokenNotice =
    "Enter a token to unlock."


wrongTokenNotice : String
wrongTokenNotice =
    "That token was not accepted. The browser is still locked."


unreachableNotice : String
unreachableNotice =
    "Unlock failed: the server could not be reached. The browser is still locked."


revokedNotice : String
revokedNotice =
    "The server rejected the credential (401). The browser is locked — unlock to continue writing."


unavailableNotice : String
unavailableNotice =
    "Write access could not be confirmed. Browsing stays available."


draftChanged : String -> State -> State
draftChanged value state =
    { state | draft = value, notice = Nothing }


{-| Route one capabilities response to discovery or validation handling.
A validation is in flight exactly when `pending` holds its candidate;
anything else is gate discovery.
-}
discovered : Int -> Result Http.Error Capabilities -> State -> State
discovered generation result state =
    if not (isCurrent generation state) then
        state

    else if state.pending /= Nothing then
        validated generation result state

    else
        case result of
            Ok capabilities ->
                if capabilities.writesRequireAuth then
                    { state
                        | status = Locked
                        , gated = True
                        , notice = Nothing
                    }

                else
                    { state
                        | status = OpenLocal
                        , gated = False
                        , token = Nothing
                        , notice = Nothing
                    }

            Err _ ->
                { state | status = Unavailable, notice = Just unavailableNotice }


{-| Start validating the draft. Returns the candidate to send, or `Nothing`
when there is nothing to validate. The draft clears at request time and
again on completion; the candidate waits in `pending` until the server
answers, and the generation moves so older responses go stale.
-}
unlockRequested : State -> ( State, Maybe String )
unlockRequested state =
    case state.status of
        Locked ->
            beginValidation state

        Unavailable ->
            beginValidation state

        _ ->
            ( state, Nothing )


beginValidation : State -> ( State, Maybe String )
beginValidation state =
    let
        candidate =
            String.trim state.draft
    in
    if candidate == "" then
        ( { state | notice = Just blankTokenNotice }, Nothing )

    else
        ( { state
            | status = Validating
            , pending = Just candidate
            , draft = ""
            , generation = state.generation + 1
            , notice = Nothing
          }
        , Just candidate
        )


{-| Settle a validation response. Success stores the pending candidate as
the credential; a 401 (or any other answer that withholds the capability)
leaves the browser locked with a fixed message that never echoes the
candidate. Stale generations are ignored, so a slow answer to an older
attempt cannot unlock or re-lock the browser.
-}
validated : Int -> Result Http.Error Capabilities -> State -> State
validated generation result state =
    if not (isCurrent generation state) then
        state

    else
        let
            candidateToken =
                state.pending

            cleared =
                { state | pending = Nothing, draft = "" }
        in
        case result of
            Ok capabilities ->
                if capabilities.writesRequireAuth && capabilities.canWrite then
                    { cleared
                        | status = Unlocked
                        , gated = True
                        , token = candidateToken
                        , notice = Nothing
                    }

                else if not capabilities.writesRequireAuth then
                    { cleared
                        | status = OpenLocal
                        , gated = False
                        , token = Nothing
                        , notice = Nothing
                    }

                else
                    { cleared
                        | status = Locked
                        , gated = True
                        , token = Nothing
                        , notice = Just wrongTokenNotice
                    }

            Err (Http.BadStatus 401) ->
                { cleared
                    | status = Locked
                    , gated = True
                    , token = Nothing
                    , notice = Just wrongTokenNotice
                }

            Err _ ->
                { cleared
                    | status = Locked
                    , gated = True
                    , token = Nothing
                    , notice = Just unreachableNotice
                }


{-| User-initiated lock. Secrets and the draft clear, the generation moves
so in-flight credentialed responses go stale, and the browser returns to
the locked (or open-local) state. Lock is not rollback: already-accepted
server writes stand and the caller reconciles public state separately.
-}
lockRequested : State -> State
lockRequested state =
    { state
        | status =
            if state.gated then
                Locked

            else
                OpenLocal
        , token = Nothing
        , pending = Nothing
        , draft = ""
        , generation = state.generation + 1
        , notice = Nothing
    }


{-| Server-initiated lock after a 401 on a credentialed request. Same
clearing as a manual lock, plus a fixed notice that never echoes secrets.
Already-accepted writes stand; the caller reconciles public state.
-}
revoked : State -> State
revoked state =
    let
        locked =
            lockRequested { state | gated = True }
    in
    { locked | notice = Just revokedNotice }


{-| Re-run gate discovery after an unavailable answer. Secrets are
untouched (there are none while unavailable), the draft stays for the
user's next attempt, and the generation moves.
-}
retryRequested : State -> State
retryRequested state =
    { state | status = Checking, generation = state.generation + 1, notice = Nothing }
