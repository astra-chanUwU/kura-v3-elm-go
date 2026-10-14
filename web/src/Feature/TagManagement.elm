module Feature.TagManagement exposing
    ( Context
    , Model
    , Msg(..)
    , OutMsg(..)
    , canConfirm
    , context
    , currentPreview
    , init
    , isOpen
    , locked
    , modeOf
    , listLoading
    , needsPreview
    , nextCursor
    , rows
    , sourceTag
    , status
    , targetTag
    , uncertain
    , update
    , view
    , writePending
    )

import Api.TagManagement
import Domain.TagManagement as Domain exposing (ListPage, Mode(..), Preview, Receipt, TagRow)
import Html exposing (Html, button, div, input, label, li, p, span, text, ul)
import Html.Attributes exposing (checked, class, disabled, name, placeholder, type_, value)
import Html.Events exposing (onClick, onInput)
import Http
import Set exposing (Set)


type alias Context =
    { apiBase : String
    , credential : Maybe String
    , writesEnabled : Bool
    , accessGeneration : Int
    }


context : String -> Maybe String -> Bool -> Int -> Context
context apiBase credential writesEnabled accessGeneration =
    { apiBase = apiBase, credential = credential, writesEnabled = writesEnabled, accessGeneration = accessGeneration }


type alias Draft =
    { mode : Mode, source : String, target : String }


type alias PreviewState =
    { draft : Draft, preview : Preview }


type Phase
    = Idle
    | Previewing
    | PreviewReady
    | Applying
    | Uncertain
    | NeedsPreview


type alias Model =
    { open : Bool
    , prefix : String
    , listPrefix : String
    , rows : List TagRow
    , nextCursor : Maybe String
    , seenCursors : Set String
    , listVersion : Maybe Int
    , pendingCursor : Maybe String
    , reloadNotice : Maybe String
    , status : Maybe String
    , listLoading : Bool
    , listRequestId : Int
    , source : String
    , target : String
    , mode : Mode
    , previewState : Maybe PreviewState
    , previewRequestId : Int
    , writeRequestId : Int
    , phase : Phase
    }


init : Model
init =
    { open = False, prefix = "", listPrefix = "", rows = [], nextCursor = Nothing, seenCursors = Set.empty, listVersion = Nothing, pendingCursor = Nothing, reloadNotice = Nothing, status = Nothing, listLoading = False, listRequestId = 0, source = "", target = "", mode = Rename, previewState = Nothing, previewRequestId = 0, writeRequestId = 0, phase = Idle }


isOpen : Model -> Bool
isOpen m =
    m.open


rows : Model -> List TagRow
rows m =
    m.rows


nextCursor : Model -> Maybe String
nextCursor m =
    m.nextCursor


status : Model -> Maybe String
status m =
    m.status


sourceTag : Model -> String
sourceTag m =
    m.source


targetTag : Model -> String
targetTag m =
    m.target


modeOf : Model -> Mode
modeOf m =
    m.mode


currentPreview : Model -> Maybe Preview
currentPreview m =
    m.previewState |> Maybe.map .preview


listLoading : Model -> Bool
listLoading m =
    m.listLoading


uncertain : Model -> Bool
uncertain m =
    m.phase == Uncertain


needsPreview : Model -> Bool
needsPreview m =
    m.phase == NeedsPreview


writePending : Model -> Bool
writePending m =
    m.phase == Applying


currentDraft : Model -> Draft
currentDraft m =
    { mode = m.mode, source = String.trim m.source, target = String.trim m.target }


canConfirm : Model -> Bool
canConfirm m =
    case ( m.phase, m.previewState ) of
        ( PreviewReady, Just state ) ->
            state.draft == currentDraft m

        _ ->
            False


locked : Model -> Model
locked m =
    { init | listRequestId = m.listRequestId + 1, previewRequestId = m.previewRequestId + 1, writeRequestId = m.writeRequestId + 1 }


type Msg
    = OpenRequested
    | CloseRequested
    | PrefixChanged String
    | ListLoaded Int Int String (Result Http.Error ListPage)
    | LoadMoreRequested
    | SourceChosen String
    | SourceChanged String
    | TargetChanged String
    | ModeChanged Mode
    | TagsChanged
    | PreviewRequested
    | PreviewCompleted Int Int Draft (Result Http.Error Preview)
    | ApplyRequested
    | ApplyCompleted Int Int Draft Preview (Result Http.Error Receipt)


type OutMsg
    = Applied Receipt
    | ResultUncertain
    | AccessRevoked


invalidatePreview : Model -> Model
invalidatePreview m =
    { m
        | previewState = Nothing
        , previewRequestId = m.previewRequestId + 1
        , phase =
            if m.phase == Uncertain then
                Uncertain

            else if m.phase == Applying then
                m.phase

            else
                Idle
    }


editingBlocked : Model -> Bool
editingBlocked m =
    m.phase == Applying


update : Context -> Msg -> Model -> ( Model, Cmd Msg, List OutMsg )
update ctx msg model =
    case msg of
        OpenRequested ->
            if not ctx.writesEnabled then
                ( { model | status = Just "Unlock to manage tags." }, Cmd.none, [] )

            else if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                let
                    id =
                        model.listRequestId + 1
                in
                ( { model | open = True, rows = [], nextCursor = Nothing, seenCursors = Set.empty, listVersion = Nothing, pendingCursor = Nothing, reloadNotice = Nothing, listPrefix = model.prefix, listLoading = True, listRequestId = id, previewState = Nothing, previewRequestId = model.previewRequestId + 1, phase = if model.phase == Uncertain then Uncertain else Idle, status = Just "Loading tags…" }
                , Api.TagManagement.list ctx.apiBase ctx.credential model.prefix Nothing 60 (ListLoaded ctx.accessGeneration id model.prefix)
                , []
                )

        CloseRequested ->
            if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                ( { model | open = False, previewState = Nothing, phase = if model.phase == Uncertain then Uncertain else Idle, listRequestId = model.listRequestId + 1, previewRequestId = model.previewRequestId + 1, listLoading = False, pendingCursor = Nothing, reloadNotice = Nothing }, Cmd.none, [] )

        PrefixChanged value ->
            if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                let
                    base =
                        invalidatePreview { model | prefix = value }

                    id =
                        base.listRequestId + 1
                in
                if not ctx.writesEnabled then
                    ( { base | rows = [], nextCursor = Nothing, listVersion = Nothing, pendingCursor = Nothing, reloadNotice = Nothing, listPrefix = value }, Cmd.none, [] )

                else
                    ( { base | rows = [], nextCursor = Nothing, seenCursors = Set.empty, listVersion = Nothing, pendingCursor = Nothing, reloadNotice = Nothing, listPrefix = value, listLoading = True, listRequestId = id, status = Just "Loading tags…" }
                    , Api.TagManagement.list ctx.apiBase ctx.credential value Nothing 60 (ListLoaded ctx.accessGeneration id value)
                    , []
                    )

        ListLoaded generation id requestedPrefix result ->
            if generation /= ctx.accessGeneration || id /= model.listRequestId || requestedPrefix /= model.prefix then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok page ->
                        case suspectPage model page of
                            Just message ->
                                ( { model | nextCursor = Nothing, listLoading = False, pendingCursor = Nothing, reloadNotice = Nothing, status = Just message }
                                , Cmd.none
                                , []
                                )

                            Nothing ->
                                let
                                    merged =
                                        if model.pendingCursor == Nothing then
                                            page.tags

                                        else
                                            model.rows ++ page.tags
                                in
                                ( { model
                                    | rows = merged
                                    , listVersion = Just page.version
                                    , nextCursor = page.nextCursor
                                    , listLoading = False
                                    , pendingCursor = Nothing
                                    , reloadNotice = Nothing
                                    , status = model.reloadNotice
                                  }
                                , Cmd.none
                                , []
                                )

                    Err (Http.BadStatus 401) ->
                        ( { model | listLoading = False, pendingCursor = Nothing, reloadNotice = Nothing }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 409) ->
                        ( { model | listLoading = False, pendingCursor = Nothing, reloadNotice = Nothing, status = Just "The tag list changed. Filter again to reload." }, Cmd.none, [] )

                    Err err ->
                        ( { model | listLoading = False, pendingCursor = Nothing, reloadNotice = Nothing, status = Just ("Tags could not be loaded (HTTP " ++ httpStatus err ++ ").") }, Cmd.none, [] )

        LoadMoreRequested ->
            if model.listLoading || editingBlocked model then
                ( model, Cmd.none, [] )

            else
                case model.nextCursor of
                    Nothing ->
                        ( { model | status = Just "No more tags." }, Cmd.none, [] )

                    Just cursor ->
                        if Set.member cursor model.seenCursors then
                            ( { model | nextCursor = Nothing, status = Just "Tag list stopped: the server repeated a cursor." }, Cmd.none, [] )

                        else
                            let
                                id =
                                    model.listRequestId + 1
                            in
                            ( { model | listLoading = True, listRequestId = id, pendingCursor = Just cursor, reloadNotice = Nothing, seenCursors = Set.insert cursor model.seenCursors, status = Just "Loading tags…" }
                            , Api.TagManagement.list ctx.apiBase ctx.credential model.prefix (Just cursor) 60 (ListLoaded ctx.accessGeneration id model.prefix)
                            , []
                            )

        SourceChosen tag ->
            if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                ( invalidatePreview { model | source = tag }, Cmd.none, [] )

        SourceChanged value ->
            if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                ( invalidatePreview { model | source = value }, Cmd.none, [] )

        TargetChanged value ->
            if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                ( invalidatePreview { model | target = value }, Cmd.none, [] )

        ModeChanged mode ->
            if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                ( invalidatePreview { model | mode = mode }, Cmd.none, [] )

        TagsChanged ->
            if editingBlocked model then
                ( model, Cmd.none, [] )

            else
                let
                    invalidated =
                        invalidatePreview model
                in
                if model.open then
                    update ctx OpenRequested invalidated

                else
                    ( invalidated, Cmd.none, [] )

        PreviewRequested ->
            previewRequested ctx model

        PreviewCompleted generation id draft result ->
            if generation /= ctx.accessGeneration || id /= model.previewRequestId || draft /= currentDraft model || model.phase /= Previewing then
                ( model, Cmd.none, [] )

            else
                case result of
                    Ok preview ->
                        if preview.mode /= draft.mode || preview.sourceTag /= draft.source || preview.targetTag /= draft.target || preview.affectedPostCount > Domain.maxSourcePosts || preview.visiblePostCount > preview.affectedPostCount then
                            ( { model | phase = Idle, previewState = Nothing, status = Just "Preview did not match the current draft. Preview again." }, Cmd.none, [] )

                        else
                            ( { model | phase = PreviewReady, previewState = Just { draft = draft, preview = preview }, status = Nothing }, Cmd.none, [] )

                    Err (Http.BadStatus 401) ->
                        ( { model | phase = Idle }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 409) ->
                        ( { model | phase = NeedsPreview, previewState = Nothing, status = Just "Tags changed. Preview again before confirming." }, Cmd.none, [] )

                    Err err ->
                        ( { model | phase = Idle, previewState = Nothing, status = Just (previewError err) }, Cmd.none, [] )

        ApplyRequested ->
            applyRequested ctx model

        ApplyCompleted generation id draft preview result ->
            if generation /= ctx.accessGeneration || id /= model.writeRequestId || model.phase /= Applying then
                ( model, Cmd.none, [] )

            else if draft /= currentDraft model then
                ( lockUncertain model, Cmd.none, [ ResultUncertain ] )

            else
                case result of
                    Ok receipt ->
                        if Domain.receiptMatchesPreview preview receipt then
                            let
                                notice =
                                    appliedMessage receipt

                                listId =
                                    model.listRequestId + 1
                            in
                            ( { model | phase = Idle, previewState = Nothing, status = Just notice, reloadNotice = Just notice, rows = [], nextCursor = Nothing, seenCursors = Set.empty, listVersion = Just receipt.version, listPrefix = model.prefix, pendingCursor = Nothing, listLoading = True, listRequestId = listId }
                            , Api.TagManagement.list ctx.apiBase ctx.credential model.prefix Nothing 60 (ListLoaded ctx.accessGeneration listId model.prefix)
                            , [ Applied receipt ]
                            )

                        else
                            ( lockUncertain model, Cmd.none, [ ResultUncertain ] )

                    Err (Http.BadStatus 401) ->
                        ( { model | phase = Idle }, Cmd.none, [ AccessRevoked ] )

                    Err (Http.BadStatus 409) ->
                        ( { model | phase = NeedsPreview, previewState = Nothing, status = Just "Tags changed elsewhere. Preview again before confirming." }, Cmd.none, [] )

                    Err (Http.BadStatus 400) ->
                        ( { model | phase = Idle, previewState = Nothing, status = Just "That rename or merge is not allowed. Check the source and target tags." }, Cmd.none, [] )

                    Err (Http.BadStatus 404) ->
                        ( { model | phase = Idle, previewState = Nothing, status = Just "The source tag no longer exists. Preview again." }, Cmd.none, [] )

                    Err _ ->
                        ( lockUncertain model, Cmd.none, [ ResultUncertain ] )


{-| A page is suspect when it cannot be trusted as a continuation of
the rows already shown. Suspect pages are never appended: loading
stops, the cursor is dropped, and the reason is reported. First-page
versus append is decided by the cursor captured when the request was
issued (pendingCursor), never by whether rows happen to be empty.
-}
suspectPage : Model -> ListPage -> Maybe String
suspectPage model page =
    let
        isAppend =
            model.pendingCursor /= Nothing

        pageTags =
            List.map .tag page.tags

        hasInternalDuplicates =
            (Set.fromList pageTags |> Set.size) /= List.length pageTags

        versionChanged =
            isAppend
                && (case model.listVersion of
                        Just version ->
                            version /= page.version

                        Nothing ->
                            True
                   )

        overlapsRows =
            isAppend && List.any (\tag -> List.member tag (List.map .tag model.rows)) pageTags

        emptyNonTerminal =
            List.isEmpty page.tags && page.nextCursor /= Nothing

        repeatedCursor =
            case page.nextCursor of
                Just cursor ->
                    Set.member cursor model.seenCursors

                Nothing ->
                    False
    in
    if List.length page.tags > 60 then
        Just "Tag list stopped: the server returned too many tags in one page."

    else if hasInternalDuplicates then
        Just "Tag list stopped: the server returned duplicate tags."

    else if versionChanged then
        Just "Tag list stopped: the tag version changed. Filter again to reload."

    else if overlapsRows then
        Just "Tag list stopped: the server returned tags that are already listed."

    else if emptyNonTerminal then
        Just "Tag list stopped: the server returned an empty page with more tags promised."

    else if repeatedCursor then
        Just "Tag list stopped: the server repeated a cursor."

    else
        Nothing


lockUncertain : Model -> Model
lockUncertain m =
    { m | phase = Uncertain, previewState = Nothing, status = Just "The result is uncertain. Preview again before submitting — do not resubmit yet." }


appliedMessage : Receipt -> String
appliedMessage receipt =
    case receipt.mode of
        Rename ->
            "Renamed " ++ receipt.sourceTag ++ " to " ++ receipt.targetTag ++ "."

        Merge ->
            "Merged " ++ receipt.sourceTag ++ " into " ++ receipt.targetTag ++ "."


previewRequested : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
previewRequested ctx model =
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to manage tags." }, Cmd.none, [] )

    else if model.phase == Applying || model.phase == Previewing then
        ( model, Cmd.none, [] )

    else
        case ( Domain.validateTag model.source, Domain.validateTag model.target ) of
            ( Err message, _ ) ->
                ( { model | status = Just message }, Cmd.none, [] )

            ( _, Err message ) ->
                ( { model | status = Just message }, Cmd.none, [] )

            ( Ok source, Ok target ) ->
                if source == target then
                    ( { model | status = Just "Source and target tags must differ." }, Cmd.none, [] )

                else
                    let
                        draft =
                            { mode = model.mode, source = source, target = target }

                        id =
                            model.previewRequestId + 1
                    in
                    ( { model | phase = Previewing, previewState = Nothing, previewRequestId = id, source = source, target = target, status = Just "Loading preview…" }
                    , Api.TagManagement.preview ctx.apiBase ctx.credential model.mode source target (PreviewCompleted ctx.accessGeneration id draft)
                    , []
                    )


applyRequested : Context -> Model -> ( Model, Cmd Msg, List OutMsg )
applyRequested ctx model =
    if not ctx.writesEnabled then
        ( { model | status = Just "Unlock to manage tags." }, Cmd.none, [] )

    else if model.phase == Applying then
        ( model, Cmd.none, [] )

    else if model.phase == Uncertain then
        ( { model | status = Just "The result is uncertain. Preview again before submitting." }, Cmd.none, [] )

    else
        case model.previewState of
            Nothing ->
                ( { model | status = Just "Preview before confirming." }, Cmd.none, [] )

            Just state ->
                if model.phase /= PreviewReady || state.draft /= currentDraft model then
                    ( { model | phase = Idle, previewState = Nothing, status = Just "The draft changed. Preview again." }, Cmd.none, [] )

                else
                    let
                        id =
                            model.writeRequestId + 1
                    in
                    ( { model | phase = Applying, writeRequestId = id, status = Just "Applying…" }
                    , Api.TagManagement.apply ctx.apiBase ctx.credential state.draft.mode state.draft.source state.draft.target state.preview.version (ApplyCompleted ctx.accessGeneration id state.draft state.preview)
                    , []
                    )


previewError : Http.Error -> String
previewError err =
    case err of
        Http.BadStatus 400 ->
            "That rename or merge is not allowed. Check the source and target tags."

        Http.BadStatus 404 ->
            "The source tag was not found."

        Http.BadStatus code ->
            "Preview failed (HTTP " ++ String.fromInt code ++ ")."

        Http.Timeout ->
            "Preview timed out. Try again."

        Http.NetworkError ->
            "Preview failed: the server could not be reached."

        _ ->
            "Preview response was invalid."


httpStatus : Http.Error -> String
httpStatus err =
    case err of
        Http.BadStatus code ->
            String.fromInt code

        _ ->
            "network"


{-| Concise management view in the Library/Loupe panel language.
-}
view : Model -> { onClose : msg, onPrefix : String -> msg, onLoadMore : msg, onChoose : String -> msg, onSource : String -> msg, onTarget : String -> msg, onMode : Mode -> msg, onPreview : msg, onApply : msg } -> Html msg
view model msgs =
    let
        busy =
            model.phase == Applying || model.phase == Previewing

        editingDisabled =
            busy

        hiddenCount preview =
            preview.affectedPostCount - preview.visiblePostCount
    in
    div [ class "panel-section tag-manage-view" ]
        ([ p [ class "panel-note" ] [ text "Rename changes a tag name throughout your library. Merge combines it with an existing tag. Hidden images and Trash are included." ]
         , label [ class "panel-note" ]
            [ text "Filter tags "
            , input [ type_ "text", value model.prefix, placeholder "Starts with…", disabled editingDisabled, onInput msgs.onPrefix ] []
            ]
         , case model.status of
            Just s ->
                p [ class "panel-note", Html.Attributes.attribute "role" "status" ] [ text s ]

            Nothing ->
                text ""
         , ul [ class "nav-list tag-manage-list" ]
            (List.map
                (\row ->
                    li [ class "nav-row tag-manage-row" ]
                        [ span [] [ text row.tag ]
                        , text " "
                        , span [ class "panel-note" ] [ text (String.fromInt row.postCount ++ " images · " ++ String.fromInt row.visiblePostCount ++ " visible") ]
                        , text " "
                        , button [ class "button button-quiet", type_ "button", onClick (msgs.onChoose row.tag), disabled editingDisabled ] [ text "Use as source" ]
                        ]
                )
                model.rows
            )
         , if List.isEmpty model.rows && not model.listLoading then
            p [ class "panel-note" ] [ text "No tags match." ]

           else
            text ""
         , p []
            [ button [ class "button button-quiet", type_ "button", onClick msgs.onLoadMore, disabled (model.nextCursor == Nothing || model.listLoading || busy) ] [ text "Load more" ]
            ]
         , p [ class "panel-note" ]
            [ label []
                [ input [ type_ "radio", name "tag-manage-mode", checked (model.mode == Rename), disabled editingDisabled, onClick (msgs.onMode Rename) ] []
                , text " Rename "
                ]
            , label []
                [ input [ type_ "radio", name "tag-manage-mode", checked (model.mode == Merge), disabled editingDisabled, onClick (msgs.onMode Merge) ] []
                , text " Merge"
                ]
            ]
         , p []
            [ label [ class "panel-note" ] [ text "Source ", input [ type_ "text", value model.source, disabled editingDisabled, onInput msgs.onSource ] [] ]
            ]
         , p []
            [ label [ class "panel-note" ] [ text "Target ", input [ type_ "text", value model.target, disabled editingDisabled, onInput msgs.onTarget ] [] ]
            ]
         , p []
            [ button [ class "button", type_ "button", onClick msgs.onPreview, disabled (busy || model.phase == Applying) ] [ text "Preview" ]
            , button [ class "button button-quiet", type_ "button", onClick msgs.onClose, disabled (model.phase == Applying) ] [ text "Close" ]
            ]
         ]
            ++ (case model.previewState of
                    Just state ->
                        if state.draft == currentDraft model && model.phase == PreviewReady then
                            [ p [ class "panel-note" ]
                                [ text
                                    ((case state.draft.mode of
                                        Rename ->
                                            "Rename "

                                        Merge ->
                                            "Merge "
                                     )
                                        ++ state.draft.source
                                        ++ " → "
                                        ++ state.draft.target
                                        ++ ": "
                                        ++ String.fromInt state.preview.affectedPostCount
                                        ++ " images affected, "
                                        ++ String.fromInt state.preview.visiblePostCount
                                        ++ " visible, "
                                        ++ String.fromInt (hiddenCount state.preview)
                                        ++ " hidden/trashed included. Target currently has "
                                        ++ String.fromInt state.preview.targetPostCount
                                        ++ " images."
                                    )
                                ]
                            , p []
                                [ button [ class "button", type_ "button", onClick msgs.onApply, disabled (not (canConfirm model)) ]
                                    [ text
                                        (case state.draft.mode of
                                            Rename ->
                                                "Confirm rename"

                                            Merge ->
                                                "Confirm merge"
                                        )
                                    ]
                                ]
                            ]

                        else
                            []

                    Nothing ->
                        []
               )
        )
