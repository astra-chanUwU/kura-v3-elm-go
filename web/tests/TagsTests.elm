module TagsTests exposing (allPassed, suite)

import Domain.Post exposing (PostDetail, TagRevision)
import Feature.Tags as Tags
import Http


unlocked : List String -> Maybe String -> Maybe PostDetail -> Tags.Context
unlocked ids active detail =
    Tags.context "" (Just "tok") True 7 ids active detail (\_ -> Nothing)


lockedCtx : Tags.Context
lockedCtx =
    Tags.context "" Nothing False 7 [ "p1" ] (Just "p1") Nothing (\_ -> Nothing)


step : Tags.Context -> Tags.Msg -> Tags.Model -> ( Tags.Model, List Tags.OutMsg )
step ctx msg model =
    let
        ( settled, _, outs ) =
            Tags.update ctx msg model
    in
    ( settled, outs )


rev : Int -> Bool -> TagRevision
rev version revertible =
    { version = version
    , kind = "tag_edit"
    , addedTags = []
    , removedTags = []
    , targetTags = Just []
    , revertible = revertible
    , createdAt = "2026-01-01"
    }


detailWith : List TagRevision -> PostDetail
detailWith history =
    { id = "p1"
    , previewUrl = "/m"
    , originalUrl = "/o"
    , mediaType = "image/jpeg"
    , width = 1
    , height = 1
    , source = ""
    , artist = ""
    , hash = "h"
    , fileSize = 1
    , createdAt = "2026-01-01"
    , tags = [ "a" ]
    , tagVersion = 4
    , favorite = False
    , score = 0
    , reactionVersion = 1
    , history = history
    , reactionHistory = []
    , derivativeJobId = Nothing
    , derivativeStatus = Nothing
    }


editCtx : Tags.Context
editCtx =
    unlocked [ "p1" ] (Just "p1") Nothing


withDrafts : Tags.Model
withDrafts =
    step editCtx (Tags.AddChanged "cat, demo") Tags.init |> Tuple.first


saving : Tags.Model
saving =
    step editCtx Tags.SaveRequested withDrafts |> Tuple.first


complete : Int -> (Tags.Model -> ( Tags.Model, List Tags.OutMsg ))
complete id =
    \model ->
        step editCtx (Tags.Completed 7 id [ "p1" ] (Ok savedResponse)) model


savedResponse : { posts : List { id : String, version : Int, tags : List String, changed : Bool } }
savedResponse =
    { posts = [ { id = "p1", version = 5, tags = [ "cat", "demo" ], changed = True } ] }


revertCtx : Tags.Context
revertCtx =
    unlocked [ "p1" ] (Just "p1") (Just (detailWith [ rev 2 True ]))


reverting : Tags.Model
reverting =
    step revertCtx (Tags.RevertRequested 2) Tags.init
        |> Tuple.first
        |> (\armed -> step revertCtx (Tags.RevertConfirmed 2) armed |> Tuple.first)


caseDraftSplit : Bool
caseDraftSplit =
    Tags.splitTagDraft "cat, demo,, cat , " == [ "cat", "demo", "cat" ]


caseSaveLocked : Bool
caseSaveLocked =
    let
        ( settled, outs ) =
            step lockedCtx Tags.SaveRequested Tags.init
    in
    outs == [] && Tags.status settled == Just "Unlock to edit tags." && not (Tags.saving settled)


caseSaveWithoutTargets : Bool
caseSaveWithoutTargets =
    let
        ( settled, outs ) =
            step (unlocked [] Nothing Nothing) Tags.SaveRequested withDrafts
    in
    outs == [] && Tags.status settled == Just "Select a post before editing tags."


caseSaveWithoutTags : Bool
caseSaveWithoutTags =
    let
        ( settled, outs ) =
            step editCtx Tags.SaveRequested Tags.init
    in
    outs == [] && Tags.status settled == Just "Enter a tag to add or remove."


caseSaveFires : Bool
caseSaveFires =
    Tags.saving saving


caseSaveBumpsGeneration : Bool
caseSaveBumpsGeneration =
    Tags.requestId saving == Tags.requestId Tags.init + 1


caseStaleGenerationReconciles : Bool
caseStaleGenerationReconciles =
    let
        ( settled, outs ) =
            step editCtx (Tags.Completed 3 (Tags.requestId saving) [ "p1" ] (Ok savedResponse)) saving
    in
    outs == [ Tags.StaleResponse [ "p1" ] ] && settled == saving


caseStaleRequestIgnored : Bool
caseStaleRequestIgnored =
    let
        ( settled, outs ) =
            step editCtx (Tags.Completed 7 999 [ "p1" ] (Ok savedResponse)) saving
    in
    outs == [] && settled == saving


caseSaveSuccess : Bool
caseSaveSuccess =
    let
        ( settled, outs ) =
            complete (Tags.requestId saving) saving
    in
    outs == [ Tags.Saved savedResponse (Just "p1") ]
        && Tags.status settled == Just "Tags saved."
        && Tags.addDraft settled == ""
        && not (Tags.saving settled)


caseRevertSuccessNotice : Bool
caseRevertSuccessNotice =
    let
        ( settled, outs ) =
            step revertCtx (Tags.Completed 7 (Tags.requestId reverting) [ "p1" ] (Ok savedResponse)) reverting
    in
    outs == [ Tags.Saved savedResponse (Just "p1") ]
        && Tags.status settled == Nothing
        && Tags.revertStatus settled == Just "Reverted to v2."


caseRevertArmsConfirmation : Bool
caseRevertArmsConfirmation =
    let
        ( settled, outs ) =
            step revertCtx (Tags.RevertRequested 2) Tags.init
    in
    outs == [] && Tags.revertConfirm settled == Just 2


caseRevertLegacyRefused : Bool
caseRevertLegacyRefused =
    let
        ctx =
            unlocked [ "p1" ] (Just "p1") (Just (detailWith [ rev 1 False ]))

        ( settled, _ ) =
            step ctx (Tags.RevertRequested 1) Tags.init
    in
    Tags.revertStatus settled == Just "Legacy revision cannot be reverted." && Tags.revertConfirm settled == Nothing


caseRevertUnknownRefused : Bool
caseRevertUnknownRefused =
    let
        ctx =
            unlocked [ "p1" ] (Just "p1") (Just (detailWith [ rev 2 True ]))

        ( settled, _ ) =
            step ctx (Tags.RevertRequested 9) Tags.init
    in
    Tags.revertStatus settled == Just "Revision not found."


caseRevertWithoutDetailArms : Bool
caseRevertWithoutDetailArms =
    let
        ( settled, _ ) =
            step editCtx (Tags.RevertRequested 2) Tags.init
    in
    Tags.revertConfirm settled == Just 2


caseRevertBlockedWhileReverting : Bool
caseRevertBlockedWhileReverting =
    let
        ( settled, _ ) =
            step revertCtx (Tags.RevertRequested 2) reverting
    in
    settled == reverting


caseRevertLocked : Bool
caseRevertLocked =
    let
        ( settled, _ ) =
            step lockedCtx (Tags.RevertRequested 2) Tags.init
    in
    Tags.revertStatus settled == Just "Unlock to revert tags."


caseConfirmWithoutActive : Bool
caseConfirmWithoutActive =
    let
        ( settled, _ ) =
            step (unlocked [ "p1" ] Nothing Nothing) (Tags.RevertConfirmed 2) Tags.init
    in
    Tags.status settled == Just "Select a post before reverting tags."


caseSaveConflict : Bool
caseSaveConflict =
    let
        ( settled, outs ) =
            step editCtx (Tags.Completed 7 (Tags.requestId saving) [ "p1" ] (Err (Http.BadStatus 409))) saving
    in
    outs == [] && Tags.status settled == Just "Tags changed elsewhere. Reload the post and try again."


caseMissingPostMessage : Bool
caseMissingPostMessage =
    Tags.errorToString (Http.BadStatus 404) == "One selected post no longer exists."


caseRevertConflictNotice : Bool
caseRevertConflictNotice =
    let
        ( settled, _ ) =
            step revertCtx (Tags.Completed 7 (Tags.requestId reverting) [ "p1" ] (Err (Http.BadStatus 409))) reverting
    in
    Tags.status settled == Nothing && Tags.revertStatus settled == Just "Tags changed elsewhere. Reload the post and try again."


caseUnauthorizedRevokes : Bool
caseUnauthorizedRevokes =
    let
        ( settled, outs ) =
            step editCtx (Tags.Completed 7 (Tags.requestId saving) [ "p1" ] (Err (Http.BadStatus 401))) saving
    in
    outs == [ Tags.AccessRevoked ] && settled == saving


caseCancelArmed : Bool
caseCancelArmed =
    let
        ( armed, _ ) =
            step revertCtx (Tags.RevertRequested 2) Tags.init

        ( settled, _ ) =
            step revertCtx Tags.RevertCancelled armed
    in
    Tags.revertConfirm settled == Nothing && Tags.revertStatus settled == Nothing


caseCancelDuringRevertKeepsPending : Bool
caseCancelDuringRevertKeepsPending =
    let
        flagged =
            step revertCtx Tags.RevertCancelled { reverting | revertStatus = Just "stale notice" } |> Tuple.first
    in
    Tags.revertPending flagged == Just 2 && Tags.revertStatus flagged == Nothing


caseActiveChangeKeepsRevert : Bool
caseActiveChangeKeepsRevert =
    let
        ( armed, _ ) =
            step revertCtx (Tags.RevertRequested 2) Tags.init
    in
    Tags.revertPending (Tags.activeChanged reverting) == Just 2 && Tags.revertConfirm (Tags.activeChanged armed) == Nothing


caseLockClears : Bool
caseLockClears =
    let
        ( armed, _ ) =
            step revertCtx (Tags.RevertRequested 2) Tags.init

        cleared =
            Tags.locked armed
    in
    Tags.revertConfirm cleared == Nothing && not (Tags.saving cleared)


caseNotedVersionsWin : Bool
caseNotedVersionsWin =
    Tags.targetsFor editCtx (Tags.noteVersion "p1" 4 Tags.init) == [ { id = "p1", version = 4 } ]


caseDetailVersionFallback : Bool
caseDetailVersionFallback =
    let
        ctx =
            Tags.context "" (Just "tok") True 7 [ "p1" ] (Just "p1") Nothing (\_ -> Just 3)
    in
    Tags.targetsFor ctx Tags.init == [ { id = "p1", version = 3 } ]


caseMissingVersionZero : Bool
caseMissingVersionZero =
    Tags.targetsFor editCtx Tags.init == [ { id = "p1", version = 0 } ]


caseSuccessClearsArmedConfirm : Bool
caseSuccessClearsArmedConfirm =
    let
        ( armed, _ ) =
            step revertCtx (Tags.RevertRequested 2) Tags.init

        drafted =
            step revertCtx (Tags.AddChanged "x") armed |> Tuple.first

        ( sent, _ ) =
            step revertCtx Tags.SaveRequested drafted

        ( settled, _ ) =
            step revertCtx (Tags.Completed 7 (Tags.requestId sent) [ "p1" ] (Ok savedResponse)) sent
    in
    Tags.revertConfirm settled == Nothing && Tags.status settled == Just "Tags saved."


caseFailureKeepsArmedConfirm : Bool
caseFailureKeepsArmedConfirm =
    let
        ( armed, _ ) =
            step revertCtx (Tags.RevertRequested 2) Tags.init

        drafted =
            step revertCtx (Tags.AddChanged "x") armed |> Tuple.first

        ( sent, _ ) =
            step revertCtx Tags.SaveRequested drafted

        ( settled, _ ) =
            step revertCtx (Tags.Completed 7 (Tags.requestId sent) [ "p1" ] (Err Http.Timeout)) sent
    in
    Tags.revertConfirm settled == Just 2 && Tags.status settled == Just "The tag edit timed out."


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "drafts split on commas", caseDraftSplit )
    , ( "locked save asks to unlock", caseSaveLocked )
    , ( "save without targets asks to select", caseSaveWithoutTargets )
    , ( "save without tags asks for input", caseSaveWithoutTags )
    , ( "valid save starts saving", caseSaveFires )
    , ( "save bumps the request generation", caseSaveBumpsGeneration )
    , ( "stale generation reconciles captured posts", caseStaleGenerationReconciles )
    , ( "stale request is ignored", caseStaleRequestIgnored )
    , ( "save success reports and clears drafts", caseSaveSuccess )
    , ( "revert success names the version", caseRevertSuccessNotice )
    , ( "revert arms confirmation", caseRevertArmsConfirmation )
    , ( "legacy revision cannot be reverted", caseRevertLegacyRefused )
    , ( "unknown revision is reported", caseRevertUnknownRefused )
    , ( "revert without detail still arms", caseRevertWithoutDetailArms )
    , ( "revert request is blocked while reverting", caseRevertBlockedWhileReverting )
    , ( "locked revert asks to unlock", caseRevertLocked )
    , ( "confirm without an active post asks to select", caseConfirmWithoutActive )
    , ( "save conflict keeps the version hint", caseSaveConflict )
    , ( "missing selected post keeps its specific message", caseMissingPostMessage )
    , ( "revert conflict lands on the revert notice", caseRevertConflictNotice )
    , ( "401 revokes access", caseUnauthorizedRevokes )
    , ( "cancel clears an armed confirmation", caseCancelArmed )
    , ( "cancel during a revert keeps it running", caseCancelDuringRevertKeepsPending )
    , ( "active change keeps an in-flight revert", caseActiveChangeKeepsRevert )
    , ( "lock clears armed and saving work", caseLockClears )
    , ( "noted versions win target resolution", caseNotedVersionsWin )
    , ( "detail versions back up target resolution", caseDetailVersionFallback )
    , ( "missing versions resolve to zero", caseMissingVersionZero )
    , ( "success clears an armed confirmation", caseSuccessClearsArmedConfirm )
    , ( "failure keeps an armed confirmation", caseFailureKeepsArmedConfirm )
    ]
