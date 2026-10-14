module ReactionsTests exposing (allPassed, suite)

import Dict
import Domain.Post exposing (PostDetail)
import Domain.Rating
import Feature.Reactions as Reactions
import Http


rctx : List String -> Maybe String -> Maybe Bool -> Reactions.Context
rctx ids active fav =
    Reactions.context "" (Just "tok") True 7 ids active fav (\_ -> Nothing)


lockedCtx : Reactions.Context
lockedCtx =
    Reactions.context "" Nothing False 7 [ "p1" ] (Just "p1") (Just True) (\_ -> Nothing)


editCtx : Reactions.Context
editCtx =
    rctx [ "p1" ] (Just "p1") (Just True)


step : Reactions.Context -> Reactions.Msg -> Reactions.Model -> ( Reactions.Model, List Reactions.OutMsg )
step ctx msg model =
    let
        ( settled, _, outs ) =
            Reactions.update ctx msg model
    in
    ( settled, outs )


detailScored : Int -> PostDetail
detailScored score =
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
    , tags = []
    , tagVersion = 1
    , favorite = False
    , score = score
    , reactionVersion = 1
    , history = []
    , reactionHistory = []
    , derivativeJobId = Nothing
    , derivativeStatus = Nothing
    , metadataVersion = 0
    , metadataHistory = []
    , rating = Domain.Rating.Unrated
    , ratingVersion = Just 0
    , ratingHistory = []
    }


savedResponse : { posts : List { id : String, version : Int, favorite : Bool, score : Int, changed : Bool } }
savedResponse =
    { posts = [ { id = "p1", version = 6, favorite = True, score = 3, changed = True } ] }


withScore : String -> Reactions.Model
withScore value =
    step editCtx (Reactions.DraftChanged value) Reactions.init |> Tuple.first


savingScore : Reactions.Model
savingScore =
    step editCtx Reactions.ScoreRequested (withScore "3") |> Tuple.first


savingFavorite : Reactions.Model
savingFavorite =
    step editCtx Reactions.FavoriteRequested Reactions.init |> Tuple.first


caseDraftChanged : Bool
caseDraftChanged =
    let
        flagged =
            { savingScore | status = Just "old" }

        ( settled, outs ) =
            step editCtx (Reactions.DraftChanged "9") flagged
    in
    outs == [] && Reactions.draft settled == "9" && Reactions.status settled == Nothing


caseStepUpSaves : Bool
caseStepUpSaves =
    let
        ( settled, outs ) =
            step editCtx (Reactions.StepScore 1) (withScore "3")
    in
    outs == [] && Reactions.draft settled == "4" && Reactions.saving settled


caseStepClampsAtZero : Bool
caseStepClampsAtZero =
    let
        ( settled, _ ) =
            step editCtx (Reactions.StepScore -1) (withScore "0")
    in
    Reactions.draft settled == "0" && Reactions.saving settled


caseStepTreatsGarbageAsZero : Bool
caseStepTreatsGarbageAsZero =
    let
        ( settled, _ ) =
            step editCtx (Reactions.StepScore 1) (withScore "abc")
    in
    Reactions.draft settled == "1"


caseStepLockedStillStepsDraft : Bool
caseStepLockedStillStepsDraft =
    let
        ( settled, outs ) =
            step lockedCtx (Reactions.StepScore 1) (withScore "3")
    in
    outs == [] && Reactions.draft settled == "4" && Reactions.status settled == Just "Unlock to change the score."


caseFavoriteLocked : Bool
caseFavoriteLocked =
    let
        ( settled, outs ) =
            step lockedCtx Reactions.FavoriteRequested Reactions.init
    in
    outs == [] && Reactions.status settled == Just "Unlock to change favorites."


caseFavoriteWithoutDetail : Bool
caseFavoriteWithoutDetail =
    let
        ( settled, outs ) =
            step (rctx [ "p1" ] (Just "p1") Nothing) Reactions.FavoriteRequested Reactions.init
    in
    outs == [] && Reactions.status settled == Just "Select a post and load its details before changing favorite."


caseFavoriteWithoutTargets : Bool
caseFavoriteWithoutTargets =
    let
        ( settled, outs ) =
            step (rctx [] (Just "p1") (Just True)) Reactions.FavoriteRequested Reactions.init
    in
    outs == [] && Reactions.status settled == Just "Select a post before changing favorite."


caseFavoriteFires : Bool
caseFavoriteFires =
    Reactions.saving savingFavorite && Reactions.requestId savingFavorite == 1


caseScoreLocked : Bool
caseScoreLocked =
    let
        ( settled, outs ) =
            step lockedCtx Reactions.ScoreRequested (withScore "3")
    in
    outs == [] && Reactions.status settled == Just "Unlock to change the score."


caseScoreInvalid : Bool
caseScoreInvalid =
    List.all
        (\value ->
            step editCtx Reactions.ScoreRequested (withScore value)
                |> Tuple.first
                |> Reactions.status
                |> (==) (Just "Score must be a non-negative integer.")
        )
        [ "abc", "", "-3" ]


caseScoreWithoutTargets : Bool
caseScoreWithoutTargets =
    let
        ( settled, outs ) =
            step (rctx [] (Just "p1") (Just True)) Reactions.ScoreRequested (withScore "3")
    in
    outs == [] && Reactions.status settled == Just "Select a post before changing score."


caseScoreFires : Bool
caseScoreFires =
    Reactions.saving savingScore && Reactions.requestId savingScore == 1


caseStaleGenerationReconciles : Bool
caseStaleGenerationReconciles =
    let
        ( settled, outs ) =
            step editCtx (Reactions.Completed 3 (Reactions.requestId savingScore) [ "p1" ] (Ok savedResponse)) savingScore
    in
    outs == [ Reactions.StaleResponse [ "p1" ] ] && settled == savingScore


caseStaleRequestIgnored : Bool
caseStaleRequestIgnored =
    let
        ( settled, outs ) =
            step editCtx (Reactions.Completed 7 999 [ "p1" ] (Ok savedResponse)) savingScore
    in
    outs == [] && settled == savingScore


caseSuccessReloadsActive : Bool
caseSuccessReloadsActive =
    let
        ( settled, outs ) =
            step editCtx (Reactions.Completed 7 (Reactions.requestId savingScore) [ "p1" ] (Ok savedResponse)) savingScore
    in
    outs == [ Reactions.Saved (Just "p1") ]
        && Reactions.status settled == Just "Favorite or score saved."
        && not (Reactions.saving settled)
        && Reactions.versions settled == Dict.fromList [ ( "p1", 6 ) ]


caseSuccessWithoutActive : Bool
caseSuccessWithoutActive =
    let
        ctx =
            rctx [ "p1" ] Nothing (Just True)

        ( sent, _ ) =
            step ctx Reactions.ScoreRequested (withScore "3")

        ( _, outs ) =
            step ctx (Reactions.Completed 7 (Reactions.requestId sent) [ "p1" ] (Ok savedResponse)) sent
    in
    outs == [ Reactions.Saved Nothing ]


caseUnauthorizedRevokes : Bool
caseUnauthorizedRevokes =
    let
        ( settled, outs ) =
            step editCtx (Reactions.Completed 7 (Reactions.requestId savingScore) [ "p1" ] (Err (Http.BadStatus 401))) savingScore
    in
    outs == [ Reactions.AccessRevoked ] && settled == savingScore


caseConflictMessage : Bool
caseConflictMessage =
    let
        ( settled, outs ) =
            step editCtx (Reactions.Completed 7 (Reactions.requestId savingScore) [ "p1" ] (Err (Http.BadStatus 409))) savingScore
    in
    outs == [] && Reactions.status settled == Just "Rating changed elsewhere. Reload the post and try again."


caseMissingPostMessage : Bool
caseMissingPostMessage =
    Reactions.errorToString (Http.BadStatus 404) == "One selected post no longer exists."


caseNotedVersionsWin : Bool
caseNotedVersionsWin =
    Reactions.targetsFor editCtx (Reactions.noteVersion "p1" 4 Reactions.init) == [ { id = "p1", version = 4 } ]


caseDetailVersionFallback : Bool
caseDetailVersionFallback =
    let
        ctx =
            Reactions.context "" (Just "tok") True 7 [ "p1" ] (Just "p1") (Just True) (\_ -> Just 3)
    in
    Reactions.targetsFor ctx Reactions.init == [ { id = "p1", version = 3 } ]


caseMissingVersionZero : Bool
caseMissingVersionZero =
    Reactions.targetsFor editCtx Reactions.init == [ { id = "p1", version = 0 } ]


caseSyncDraftFollowsDetail : Bool
caseSyncDraftFollowsDetail =
    Reactions.draft (Reactions.syncDraft (Just (detailScored 8)) Reactions.init) == "8"
        && Reactions.draft (Reactions.syncDraft Nothing Reactions.init) == "0"


caseActiveChangeResetsNotice : Bool
caseActiveChangeResetsNotice =
    let
        flagged =
            { savingScore | status = Just "old" }

        changed =
            Reactions.activeChanged (Just (detailScored 5)) flagged
    in
    Reactions.status changed == Nothing && Reactions.draft changed == "5" && Reactions.saving changed


caseLockKeepsDraftAndVersions : Bool
caseLockKeepsDraftAndVersions =
    let
        noted =
            Reactions.noteVersion "p1" 4 (withScore "3")

        cleared =
            Reactions.locked { noted | saving = True }
    in
    not (Reactions.saving cleared)
        && Reactions.draft cleared == "3"
        && Reactions.versions cleared == Dict.fromList [ ( "p1", 4 ) ]


allPassed : Bool
allPassed =
    List.all Tuple.second suite


suite : List ( String, Bool )
suite =
    [ ( "draft edits clear the notice", caseDraftChanged )
    , ( "step up saves the stepped score", caseStepUpSaves )
    , ( "step down clamps at zero", caseStepClampsAtZero )
    , ( "step treats garbage as zero", caseStepTreatsGarbageAsZero )
    , ( "locked step still steps the draft", caseStepLockedStillStepsDraft )
    , ( "locked favorite asks to unlock", caseFavoriteLocked )
    , ( "favorite without detail asks to load", caseFavoriteWithoutDetail )
    , ( "favorite without targets asks to select", caseFavoriteWithoutTargets )
    , ( "favorite fires with a bumped generation", caseFavoriteFires )
    , ( "locked score asks to unlock", caseScoreLocked )
    , ( "invalid scores are rejected", caseScoreInvalid )
    , ( "score without targets asks to select", caseScoreWithoutTargets )
    , ( "valid score starts saving", caseScoreFires )
    , ( "stale generation reconciles captured posts", caseStaleGenerationReconciles )
    , ( "stale request is ignored", caseStaleRequestIgnored )
    , ( "success reports and merges versions", caseSuccessReloadsActive )
    , ( "success without active still settles", caseSuccessWithoutActive )
    , ( "401 revokes access", caseUnauthorizedRevokes )
    , ( "conflict keeps the rating hint", caseConflictMessage )
    , ( "missing selected post keeps its specific message", caseMissingPostMessage )
    , ( "noted versions win target resolution", caseNotedVersionsWin )
    , ( "detail versions back up target resolution", caseDetailVersionFallback )
    , ( "missing versions resolve to zero", caseMissingVersionZero )
    , ( "draft follows the loaded detail", caseSyncDraftFollowsDetail )
    , ( "active change resets notice and draft", caseActiveChangeResetsNotice )
    , ( "lock keeps draft and versions", caseLockKeepsDraftAndVersions )
    ]
