module AccessTests exposing (allPassed, suite)

{-| Focused checks for the unlock/lock boundary: capability decoding,
header selection, the transient access state machine, owner separation
for saved searches, and secret hygiene in user-facing messages.
-}

import Api.Access as Access
import Api.SavedSearch as SavedSearch
import App.Access as AppAccess
import Http
import Json.Decode as Decode


openCapabilities : Access.Capabilities
openCapabilities =
    { writesRequireAuth = False, canWrite = True, actor = Just "local" }


lockedCapabilities : Access.Capabilities
lockedCapabilities =
    { writesRequireAuth = True, canWrite = False, actor = Nothing }


unlockedCapabilities : Access.Capabilities
unlockedCapabilities =
    { writesRequireAuth = True, canWrite = True, actor = Just "system" }


candidate : String
candidate =
    "s3krit-token-xyz"


noticesFor : String -> List String
noticesFor token =
    let
        base =
            AppAccess.init

        locked =
            { base | status = AppAccess.Locked, draft = token }

        ( validating, _ ) =
            AppAccess.unlockRequested locked

        wrong =
            AppAccess.validated validating.generation (Err (Http.BadStatus 401)) validating

        unreachable =
            AppAccess.validated validating.generation (Err Http.Timeout) validating

        revoked =
            AppAccess.revoked { validating | status = AppAccess.Unlocked, token = Just token }
    in
    List.filterMap identity [ wrong.notice, unreachable.notice, revoked.notice ]


caseOpenDecodes : Bool
caseOpenDecodes =
    case Decode.decodeString Access.capabilitiesDecoder """{"writes_require_auth":false,"can_write":true,"actor":"local"}""" of
        Ok capabilities ->
            capabilities == openCapabilities

        Err _ ->
            False


caseLockedDecodes : Bool
caseLockedDecodes =
    case Decode.decodeString Access.capabilitiesDecoder """{"writes_require_auth":true,"can_write":false,"actor":null}""" of
        Ok capabilities ->
            capabilities == lockedCapabilities

        Err _ ->
            False


caseUnlockedDecodes : Bool
caseUnlockedDecodes =
    case Decode.decodeString Access.capabilitiesDecoder """{"writes_require_auth":true,"can_write":true,"actor":"system"}""" of
        Ok capabilities ->
            capabilities == unlockedCapabilities

        Err _ ->
            False


caseCapabilitiesRejectBadShapes : Bool
caseCapabilitiesRejectBadShapes =
    List.all isErr
        [ Decode.decodeString Access.capabilitiesDecoder """{"can_write":true,"actor":"local"}"""
        , Decode.decodeString Access.capabilitiesDecoder """{"writes_require_auth":"yes","can_write":true,"actor":null}"""
        , Decode.decodeString Access.capabilitiesDecoder """not json"""
        ]


isErr : Result e a -> Bool
isErr result =
    case result of
        Err _ ->
            True

        Ok _ ->
            False


caseHeaderSelection : Bool
caseHeaderSelection =
    Access.authHeaderValue Nothing
        == Nothing
        && Access.authHeaderValue (Just "")
        == Nothing
        && Access.authHeaderValue (Just "   ")
        == Nothing
        && Access.authHeaderValue (Just "abc")
        == Just "Bearer abc"
        && Access.authHeaderValue (Just "  abc  ")
        == Just "Bearer abc"
        && List.length (Access.authHeaders Nothing)
        == 0
        && List.length (Access.authHeaders (Just ""))
        == 0
        && List.length (Access.authHeaders (Just "abc"))
        == 1


caseInitDenied : Bool
caseInitDenied =
    AppAccess.init.status
        == AppAccess.Checking
        && not (AppAccess.canWrite AppAccess.init)
        && AppAccess.credential AppAccess.init
        == Nothing


caseDiscoverOpen : Bool
caseDiscoverOpen =
    let
        next =
            AppAccess.discovered 0 (Ok openCapabilities) AppAccess.init
    in
    next.status == AppAccess.OpenLocal && AppAccess.canWrite next && AppAccess.credential next == Nothing


caseDiscoverLocked : Bool
caseDiscoverLocked =
    let
        next =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init
    in
    next.status == AppAccess.Locked && not (AppAccess.canWrite next)


caseDiscoverStaleIgnored : Bool
caseDiscoverStaleIgnored =
    AppAccess.discovered 7 (Ok openCapabilities) AppAccess.init == AppAccess.init


caseDiscoverFailureUnavailable : Bool
caseDiscoverFailureUnavailable =
    let
        next =
            AppAccess.discovered 0 (Err Http.NetworkError) AppAccess.init
    in
    next.status == AppAccess.Unavailable && not (AppAccess.canWrite next)


caseBlankDraftStaysLocked : Bool
caseBlankDraftStaysLocked =
    let
        locked =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init

        ( next, sent ) =
            AppAccess.unlockRequested { locked | draft = "   " }
    in
    next.status == AppAccess.Locked && sent == Nothing && next.notice /= Nothing


caseValidTokenUnlocks : Bool
caseValidTokenUnlocks =
    let
        locked =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init

        ( validating, sent ) =
            AppAccess.unlockRequested { locked | draft = "  " ++ candidate ++ "  " }

        unlocked =
            AppAccess.validated validating.generation (Ok unlockedCapabilities) validating
    in
    sent
        == Just candidate
        && validating.status
        == AppAccess.Validating
        && validating.draft
        == ""
        && unlocked.status
        == AppAccess.Unlocked
        && AppAccess.canWrite unlocked
        && AppAccess.credential unlocked
        == Just candidate
        && unlocked.notice
        == Nothing


caseWrongTokenStaysLocked : Bool
caseWrongTokenStaysLocked =
    let
        locked =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init

        ( validating, _ ) =
            AppAccess.unlockRequested { locked | draft = candidate }

        next =
            AppAccess.validated validating.generation (Err (Http.BadStatus 401)) validating
    in
    next.status == AppAccess.Locked && not (AppAccess.canWrite next) && AppAccess.credential next == Nothing && next.draft == ""


caseUnreachableStaysLocked : Bool
caseUnreachableStaysLocked =
    let
        locked =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init

        ( validating, _ ) =
            AppAccess.unlockRequested { locked | draft = candidate }

        next =
            AppAccess.validated validating.generation (Err Http.Timeout) validating
    in
    next.status == AppAccess.Locked && AppAccess.credential next == Nothing


caseStaleValidationIgnored : Bool
caseStaleValidationIgnored =
    let
        locked =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init

        ( validating, _ ) =
            AppAccess.unlockRequested { locked | draft = candidate }

        stale =
            AppAccess.validated validating.generation (Ok unlockedCapabilities) { validating | generation = validating.generation + 10 }
    in
    stale.status == AppAccess.Validating && AppAccess.credential stale == Nothing


caseUnlockOnlyFromLockedOrUnavailable : Bool
caseUnlockOnlyFromLockedOrUnavailable =
    let
        open =
            AppAccess.discovered 0 (Ok openCapabilities) AppAccess.init
    in
    AppAccess.unlockRequested open
        == ( open, Nothing )
        && AppAccess.unlockRequested AppAccess.init
        == ( AppAccess.init, Nothing )


caseLockClearsSecrets : Bool
caseLockClearsSecrets =
    let
        locked =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init

        ( validating, _ ) =
            AppAccess.unlockRequested { locked | draft = candidate }

        unlocked =
            AppAccess.validated validating.generation (Ok unlockedCapabilities) validating

        relocked =
            AppAccess.lockRequested unlocked
    in
    relocked.status
        == AppAccess.Locked
        && relocked.token
        == Nothing
        && relocked.draft
        == ""
        && relocked.generation
        == unlocked.generation
        + 1
        && not (AppAccess.canWrite relocked)


caseLockOpenLocalStaysOpen : Bool
caseLockOpenLocalStaysOpen =
    let
        open =
            AppAccess.discovered 0 (Ok openCapabilities) AppAccess.init

        relocked =
            AppAccess.lockRequested open
    in
    relocked.status == AppAccess.OpenLocal && AppAccess.canWrite relocked


caseRevokedLocksWithNotice : Bool
caseRevokedLocksWithNotice =
    let
        locked =
            AppAccess.discovered 0 (Ok lockedCapabilities) AppAccess.init

        ( validating, _ ) =
            AppAccess.unlockRequested { locked | draft = candidate }

        unlocked =
            AppAccess.validated validating.generation (Ok unlockedCapabilities) validating

        relocked =
            AppAccess.revoked unlocked
    in
    relocked.status == AppAccess.Locked && relocked.token == Nothing && relocked.notice /= Nothing


caseRetryRechecks : Bool
caseRetryRechecks =
    let
        unavailable =
            AppAccess.discovered 0 (Err Http.Timeout) AppAccess.init

        retrying =
            AppAccess.retryRequested unavailable
    in
    retrying.status == AppAccess.Checking && retrying.generation == unavailable.generation + 1


caseOpenLocalRevocationLocks : Bool
caseOpenLocalRevocationLocks =
    let
        open =
            AppAccess.discovered 0 (Ok openCapabilities) AppAccess.init

        rejected =
            AppAccess.revoked open
    in
    rejected.status == AppAccess.Locked && rejected.gated && not (AppAccess.canWrite rejected) && rejected.token == Nothing


caseCanWriteMatrix : Bool
caseCanWriteMatrix =
    let
        base =
            AppAccess.init
    in
    List.all (\( status, expected ) -> AppAccess.canWrite { base | status = status } == expected)
        [ ( AppAccess.Checking, False )
        , ( AppAccess.OpenLocal, True )
        , ( AppAccess.Locked, False )
        , ( AppAccess.Validating, False )
        , ( AppAccess.Unlocked, True )
        , ( AppAccess.Unavailable, False )
        ]


caseNoticesNeverCarrySecrets : Bool
caseNoticesNeverCarrySecrets =
    case noticesFor candidate of
        [] ->
            False

        notices ->
            List.all (\notice -> not (String.contains candidate notice)) notices


serverEntry : String -> String -> SavedSearch.SavedSearch
serverEntry id query =
    { id = id, name = query, query = query, createdAt = "", updatedAt = "" }


caseOwnerSeparation : Bool
caseOwnerSeparation =
    SavedSearch.visible True [ serverEntry "9" "cat" ] [ "dog", "bird" ]
        == [ { id = "dog", label = "dog" }, { id = "bird", label = "bird" } ]
        && SavedSearch.visible False [ serverEntry "9" "cat" ] [ "dog" ]
        == [ { id = "9", label = "cat" } ]
        && SavedSearch.visible True [] []
        == []
        && SavedSearch.visible False [] []
        == []


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "open capabilities decode", caseOpenDecodes )
    , ( "locked capabilities decode", caseLockedDecodes )
    , ( "unlocked capabilities decode", caseUnlockedDecodes )
    , ( "capabilities reject bad shapes", caseCapabilitiesRejectBadShapes )
    , ( "auth header selection", caseHeaderSelection )
    , ( "checking denies writes", caseInitDenied )
    , ( "discover open-local", caseDiscoverOpen )
    , ( "discover locked", caseDiscoverLocked )
    , ( "stale discovery ignored", caseDiscoverStaleIgnored )
    , ( "discovery failure is unavailable", caseDiscoverFailureUnavailable )
    , ( "blank draft stays locked", caseBlankDraftStaysLocked )
    , ( "valid token unlocks", caseValidTokenUnlocks )
    , ( "wrong token stays locked", caseWrongTokenStaysLocked )
    , ( "unreachable validation stays locked", caseUnreachableStaysLocked )
    , ( "stale validation ignored", caseStaleValidationIgnored )
    , ( "unlock only from locked or unavailable", caseUnlockOnlyFromLockedOrUnavailable )
    , ( "lock clears secrets", caseLockClearsSecrets )
    , ( "lock in open-local stays open", caseLockOpenLocalStaysOpen )
    , ( "401 revokes with notice", caseRevokedLocksWithNotice )
    , ( "401 after open mode locks writes", caseOpenLocalRevocationLocks )
    , ( "retry rechecks", caseRetryRechecks )
    , ( "write matrix", caseCanWriteMatrix )
    , ( "notices never carry secrets", caseNoticesNeverCarrySecrets )
    , ( "saved-search owner separation", caseOwnerSeparation )
    ]
